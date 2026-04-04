package tray

import (
	"fmt"
	"log"
	"os"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"

	"taskflow-desktop/internal/config"
	"taskflow-desktop/internal/state"
)

var (
	user32           = windows.NewLazySystemDLL("user32.dll")
	shell32          = windows.NewLazySystemDLL("shell32.dll")
	pShellNotifyIcon = shell32.NewProc("Shell_NotifyIconW")
	pCreateWindowEx  = user32.NewProc("CreateWindowExW")
	pDefWindowProc   = user32.NewProc("DefWindowProcW")
	pRegisterClass   = user32.NewProc("RegisterClassExW")
	pGetMessage      = user32.NewProc("GetMessageW")
	pTranslateMessage = user32.NewProc("TranslateMessage")
	pDispatchMessage  = user32.NewProc("DispatchMessageW")
	pPostQuitMessage  = user32.NewProc("PostQuitMessage")
	pCreatePopupMenu  = user32.NewProc("CreatePopupMenu")
	pAppendMenu       = user32.NewProc("AppendMenuW")
	pTrackPopupMenu   = user32.NewProc("TrackPopupMenu")
	pDestroyMenu      = user32.NewProc("DestroyMenu")
	pSetForegroundWindow = user32.NewProc("SetForegroundWindow")
	pGetCursorPos     = user32.NewProc("GetCursorPos")
	pPostMessage      = user32.NewProc("PostMessageW")
	pLoadImage        = user32.NewProc("LoadImageW")
	pGetModuleHandle  = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetModuleHandleW")
	pExtractIconEx    = shell32.NewProc("ExtractIconExW")

	// GDI for drawing recording dot overlay
	gdi32              = windows.NewLazySystemDLL("gdi32.dll")
	pCreateCompatibleDC = gdi32.NewProc("CreateCompatibleDC")
	pDeleteDC           = gdi32.NewProc("DeleteDC")
	pSelectObject       = gdi32.NewProc("SelectObject")
	pCreateSolidBrush   = gdi32.NewProc("CreateSolidBrush")
	pDeleteObject       = gdi32.NewProc("DeleteObject")
	pEllipse            = gdi32.NewProc("Ellipse")
	pGetIconInfo        = user32.NewProc("GetIconInfo")
	pCreateIconIndirect = user32.NewProc("CreateIconIndirect")
	pDestroyIcon        = user32.NewProc("DestroyIcon")
	pGetDC              = user32.NewProc("GetDC")
	pReleaseDC          = user32.NewProc("ReleaseDC")
	pCreateCompatibleBitmap = gdi32.NewProc("CreateCompatibleBitmap")
	pDrawIconEx         = user32.NewProc("DrawIconEx")
	pFillRect           = user32.NewProc("FillRect")
	pCreateRectRgn      = gdi32.NewProc("CreateRectRgn")
	pFillRgn            = gdi32.NewProc("FillRgn")
)

const (
	NIM_ADD    = 0x00
	NIM_MODIFY = 0x01
	NIM_DELETE = 0x02
	NIF_MESSAGE = 0x01
	NIF_ICON    = 0x02
	NIF_TIP     = 0x04
	NIF_INFO    = 0x10

	WM_APP           = 0x8000
	WM_TRAY_CALLBACK = WM_APP + 1
	WM_COMMAND       = 0x0111
	WM_RBUTTONUP     = 0x0205
	WM_LBUTTONDBLCLK = 0x0203

	TPM_BOTTOMALIGN = 0x0020
	TPM_LEFTALIGN   = 0x0000

	MF_STRING    = 0x0000
	MF_SEPARATOR = 0x0800
	MF_GRAYED    = 0x0001

	IDM_SHOW     = 1001
	IDM_STOP     = 1002
	IDM_DASHBOARD = 1003
	IDM_QUIT     = 1004
	IDM_STATUS   = 1005
)

// NOTIFYICONDATAW is the Windows tray icon structure (extended for balloon notifications).
type NOTIFYICONDATAW struct {
	CbSize           uint32
	HWnd             uintptr
	UID              uint32
	UFlags           uint32
	UCallbackMessage uint32
	HIcon            uintptr
	SzTip            [128]uint16
	DwState          uint32
	DwStateMask      uint32
	SzInfo           [256]uint16
	UVersion         uint32
	SzInfoTitle      [64]uint16
	DwInfoFlags      uint32
}

type POINT struct {
	X, Y int32
}

type MSG struct {
	HWnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      POINT
}

type WNDCLASSEX struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     uintptr
	HIcon         uintptr
	HCursor       uintptr
	HbrBackground uintptr
	LpszMenuName  uintptr
	LpszClassName uintptr
	HIconSm       uintptr
}

// ICONINFO is the Windows icon info structure.
type ICONINFO struct {
	FIcon    int32
	XHotspot uint32
	YHotspot uint32
	HbmMask  uintptr
	HbmColor uintptr
}

// RECT structure
type RECT struct {
	Left, Top, Right, Bottom int32
}

// ActionHandler defines callbacks for tray menu actions.
type ActionHandler struct {
	OnShowWindow func()
	OnStopTimer  func()
	OnQuit       func()
}

// Manager manages the system tray icon.
type Manager struct {
	mu          sync.Mutex
	appState    *state.AppState
	handler     *ActionHandler
	hwnd        uintptr
	nid         NOTIFYICONDATAW
	running     bool
	timerActive bool
	statusText  string
	baseIcon    uintptr // Original app icon
	activeIcon  uintptr // Icon with green recording dot
}

// NewManager creates a new tray manager.
func NewManager(appState *state.AppState) *Manager {
	return &Manager{
		appState:   appState,
		statusText: "Timer stopped",
	}
}

// SetHandler sets the action callbacks.
func (m *Manager) SetHandler(h *ActionHandler) {
	m.handler = h
}

// global reference so the window proc callback can access the manager
var globalManager *Manager

// Start initializes the system tray. Must be called from a goroutine.
func (m *Manager) Start(done <-chan struct{}) {
	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return
	}
	m.running = true
	globalManager = m
	m.mu.Unlock()

	// Register window class
	className, _ := syscall.UTF16PtrFromString("TaskFlowTrayClass")
	wc := WNDCLASSEX{
		LpfnWndProc:   syscall.NewCallback(trayWndProc),
		LpszClassName: uintptr(unsafe.Pointer(className)),
	}
	wc.CbSize = uint32(unsafe.Sizeof(wc))
	pRegisterClass.Call(uintptr(unsafe.Pointer(&wc)))

	// Create hidden message-only window
	hwnd, _, _ := pCreateWindowEx.Call(
		0,
		uintptr(unsafe.Pointer(className)),
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
	)
	m.hwnd = hwnd

	// Load icons
	m.baseIcon = loadAppIcon()
	m.activeIcon = createDotOverlayIcon(m.baseIcon)

	// Create tray icon
	m.nid = NOTIFYICONDATAW{
		HWnd:             hwnd,
		UID:              1,
		UFlags:           NIF_MESSAGE | NIF_TIP | NIF_ICON,
		UCallbackMessage: WM_TRAY_CALLBACK,
		HIcon:            m.baseIcon,
	}
	m.nid.CbSize = uint32(unsafe.Sizeof(m.nid))
	copy(m.nid.SzTip[:], utf16("TaskFlow Desktop"))
	pShellNotifyIcon.Call(NIM_ADD, uintptr(unsafe.Pointer(&m.nid)))

	// Set version 4 for modern balloon/toast support on Windows 10/11
	m.nid.UVersion = 4 // NOTIFYICON_VERSION_4
	pShellNotifyIcon.Call(0x04, uintptr(unsafe.Pointer(&m.nid))) // NIM_SETVERSION

	log.Println("System tray started")

	// Message loop — runs until WM_QUIT
	var msg MSG
	for {
		select {
		case <-done:
			m.cleanup()
			return
		default:
		}

		ret, _, _ := pGetMessage.Call(
			uintptr(unsafe.Pointer(&msg)),
			0, 0, 0,
		)
		if ret == 0 { // WM_QUIT
			break
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		pDispatchMessage.Call(uintptr(unsafe.Pointer(&msg)))
	}

	m.cleanup()
}

func (m *Manager) cleanup() {
	pShellNotifyIcon.Call(NIM_DELETE, uintptr(unsafe.Pointer(&m.nid)))
	m.mu.Lock()
	m.running = false
	m.mu.Unlock()
	log.Println("System tray stopped")
}

// Stop shuts down the system tray.
func (m *Manager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.running {
		return
	}
	pPostMessage.Call(m.hwnd, 0x0012, 0, 0) // WM_QUIT via WM_CLOSE
}

// ShowBalloon displays a Windows notification balloon from the tray icon.
func (m *Manager) ShowBalloon(title, message string) {
	if !m.running {
		log.Println("ShowBalloon skipped: tray not running")
		return
	}
	log.Printf("ShowBalloon: %s - %s", title, message)

	// Clear previous balloon text first
	for i := range m.nid.SzInfoTitle {
		m.nid.SzInfoTitle[i] = 0
	}
	for i := range m.nid.SzInfo {
		m.nid.SzInfo[i] = 0
	}

	copy(m.nid.SzInfoTitle[:], utf16(title))
	copy(m.nid.SzInfo[:], utf16(message))
	m.nid.DwInfoFlags = 0x00000001 // NIIF_INFO
	// NIF_INFO is required for balloon; include NIF_ICON + NIF_TIP to keep icon visible
	m.nid.UFlags = NIF_INFO | NIF_ICON | NIF_TIP
	ret, _, err := pShellNotifyIcon.Call(NIM_MODIFY, uintptr(unsafe.Pointer(&m.nid)))
	log.Printf("ShowBalloon result: ret=%d err=%v", ret, err)
}

// SetTimerActive updates the tray icon and tooltip based on timer state.
func (m *Manager) SetTimerActive(active bool, task *state.CurrentTask) {
	m.mu.Lock()
	m.timerActive = active
	if active && task != nil {
		m.statusText = fmt.Sprintf("Working: %s", task.TaskTitle)
	} else {
		m.statusText = "Timer stopped"
	}
	m.mu.Unlock()

	// Update tooltip
	tip := fmt.Sprintf("TaskFlow — %s", m.statusText)
	if len(tip) > 127 {
		tip = tip[:127]
	}
	copy(m.nid.SzTip[:], utf16(tip))

	// Swap icon: green dot when active, normal when stopped
	if active && m.activeIcon != 0 {
		m.nid.HIcon = m.activeIcon
	} else if m.baseIcon != 0 {
		m.nid.HIcon = m.baseIcon
	}

	m.nid.UFlags = NIF_TIP | NIF_ICON
	pShellNotifyIcon.Call(NIM_MODIFY, uintptr(unsafe.Pointer(&m.nid)))
}

// trayWndProc handles window messages for the tray icon.
func trayWndProc(hwnd, msg, wParam, lParam uintptr) uintptr {
	switch msg {
	case WM_TRAY_CALLBACK:
		switch lParam {
		case WM_RBUTTONUP:
			showContextMenu(hwnd)
		case WM_LBUTTONDBLCLK:
			if globalManager != nil && globalManager.handler != nil && globalManager.handler.OnShowWindow != nil {
				globalManager.handler.OnShowWindow()
			}
		}
		return 0

	case WM_COMMAND:
		id := wParam & 0xFFFF
		switch id {
		case IDM_SHOW:
			if globalManager != nil && globalManager.handler != nil && globalManager.handler.OnShowWindow != nil {
				globalManager.handler.OnShowWindow()
			}
		case IDM_STOP:
			if globalManager != nil && globalManager.handler != nil && globalManager.handler.OnStopTimer != nil {
				globalManager.handler.OnStopTimer()
			}
		case IDM_DASHBOARD:
			openBrowser(config.Get().WebDashboardURL)
		case IDM_QUIT:
			if globalManager != nil && globalManager.handler != nil && globalManager.handler.OnQuit != nil {
				globalManager.handler.OnQuit()
			}
		}
		return 0
	}

	ret, _, _ := pDefWindowProc.Call(hwnd, msg, wParam, lParam)
	return ret
}

func showContextMenu(hwnd uintptr) {
	hmenu, _, _ := pCreatePopupMenu.Call()

	// Status line (grayed out — display only)
	status := "Timer stopped"
	if globalManager != nil {
		globalManager.mu.Lock()
		status = globalManager.statusText
		isActive := globalManager.timerActive
		globalManager.mu.Unlock()

		appendMenu(hmenu, MF_STRING|MF_GRAYED, IDM_STATUS, status)
		appendMenu(hmenu, MF_SEPARATOR, 0, "")
		appendMenu(hmenu, MF_STRING, IDM_SHOW, "Show Window")
		if isActive {
			appendMenu(hmenu, MF_STRING, IDM_STOP, "Stop Timer")
		}
	} else {
		appendMenu(hmenu, MF_STRING, IDM_SHOW, "Show Window")
	}

	appendMenu(hmenu, MF_SEPARATOR, 0, "")
	appendMenu(hmenu, MF_STRING, IDM_DASHBOARD, "Open Dashboard")
	appendMenu(hmenu, MF_SEPARATOR, 0, "")
	appendMenu(hmenu, MF_STRING, IDM_QUIT, "Quit")

	// Show menu at cursor position
	var pt POINT
	pGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	pSetForegroundWindow.Call(hwnd)
	pTrackPopupMenu.Call(hmenu, TPM_BOTTOMALIGN|TPM_LEFTALIGN, uintptr(pt.X), uintptr(pt.Y), 0, hwnd, 0)
	pDestroyMenu.Call(hmenu)
}

func appendMenu(hmenu uintptr, flags, id uintptr, text string) {
	t, _ := syscall.UTF16PtrFromString(text)
	pAppendMenu.Call(hmenu, flags, id, uintptr(unsafe.Pointer(t)))
}

func utf16(s string) []uint16 {
	r, _ := syscall.UTF16FromString(s)
	return r
}

func openBrowser(url string) {
	cmd, _ := syscall.UTF16PtrFromString(url)
	shell32.NewProc("ShellExecuteW").Call(0, 0, uintptr(unsafe.Pointer(cmd)), 0, 0, 1)
}

// loadAppIcon extracts the icon from the running exe (Wails embeds it at resource ID 3).
func loadAppIcon() uintptr {
	const IMAGE_ICON = 1
	const LR_DEFAULTSIZE = 0x0040
	const LR_SHARED = 0x8000

	hModule, _, _ := pGetModuleHandle.Call(0)
	if hModule != 0 {
		icon, _, _ := pLoadImage.Call(
			hModule, 3, IMAGE_ICON, 16, 16, LR_DEFAULTSIZE|LR_SHARED,
		)
		if icon != 0 {
			return icon
		}
	}

	exePath, err := os.Executable()
	if err == nil {
		p, _ := syscall.UTF16PtrFromString(exePath)
		var small uintptr
		pExtractIconEx.Call(uintptr(unsafe.Pointer(p)), 0, 0, uintptr(unsafe.Pointer(&small)), 1)
		if small != 0 {
			return small
		}
	}

	return 0
}

// createDotOverlayIcon draws a green recording dot on the bottom-right of the base icon.
func createDotOverlayIcon(baseIcon uintptr) uintptr {
	if baseIcon == 0 {
		return 0
	}

	const DI_NORMAL = 0x0003
	const size = 16

	// Get screen DC
	screenDC, _, _ := pGetDC.Call(0)
	if screenDC == 0 {
		return 0
	}
	defer pReleaseDC.Call(0, screenDC)

	// Create color bitmap
	hbmColor, _, _ := pCreateCompatibleBitmap.Call(screenDC, size, size)
	if hbmColor == 0 {
		return 0
	}

	// Create mask bitmap
	hbmMask, _, _ := pCreateCompatibleBitmap.Call(screenDC, size, size)
	if hbmMask == 0 {
		pDeleteObject.Call(hbmColor)
		return 0
	}

	// Create DC and draw into color bitmap
	memDC, _, _ := pCreateCompatibleDC.Call(screenDC)
	if memDC == 0 {
		pDeleteObject.Call(hbmColor)
		pDeleteObject.Call(hbmMask)
		return 0
	}
	defer pDeleteDC.Call(memDC)

	// Select color bitmap and draw base icon
	pSelectObject.Call(memDC, hbmColor)
	pDrawIconEx.Call(memDC, 0, 0, baseIcon, size, size, 0, 0, DI_NORMAL)

	// Draw green dot (bottom-right corner)
	// Red: RGB(239, 68, 68) = 0x004444EF in BGR format for Windows
	greenBrush, _, _ := pCreateSolidBrush.Call(0x004444EF)
	if greenBrush != 0 {
		// Draw filled circle at bottom-right (dot: 6px diameter)
		dotSize := int32(6)
		x := int32(size) - dotSize
		y := int32(size) - dotSize
		pSelectObject.Call(memDC, greenBrush)
		pEllipse.Call(memDC, uintptr(x), uintptr(y), uintptr(int32(size)), uintptr(int32(size)))

		// White border around dot
		whiteBrush, _, _ := pCreateSolidBrush.Call(0x00FFFFFF)
		if whiteBrush != 0 {
			// Slightly larger circle behind for border effect (drawn first next time)
			pDeleteObject.Call(whiteBrush)
		}
		pDeleteObject.Call(greenBrush)
	}

	// Setup mask (all black = fully opaque)
	maskDC, _, _ := pCreateCompatibleDC.Call(screenDC)
	if maskDC != 0 {
		pSelectObject.Call(maskDC, hbmMask)
		blackBrush, _, _ := pCreateSolidBrush.Call(0x00000000)
		if blackBrush != 0 {
			r := RECT{0, 0, size, size}
			pFillRect.Call(maskDC, uintptr(unsafe.Pointer(&r)), blackBrush)
			pDeleteObject.Call(blackBrush)
		}
		pDeleteDC.Call(maskDC)
	}

	// Create icon from bitmaps
	ii := ICONINFO{
		FIcon:    1, // TRUE = icon
		HbmMask:  hbmMask,
		HbmColor: hbmColor,
	}
	newIcon, _, _ := pCreateIconIndirect.Call(uintptr(unsafe.Pointer(&ii)))

	// Don't delete bitmaps — they're owned by the icon now
	return newIcon
}
