package tray

import (
	"fmt"
	"log"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"

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
)

const (
	NIM_ADD    = 0x00
	NIM_MODIFY = 0x01
	NIM_DELETE = 0x02
	NIF_MESSAGE = 0x01
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

// NOTIFYICONDATAW is the Windows tray icon structure.
type NOTIFYICONDATAW struct {
	CbSize           uint32
	HWnd             uintptr
	UID              uint32
	UFlags           uint32
	UCallbackMessage uint32
	HIcon            uintptr
	SzTip            [128]uint16
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

	// Create tray icon
	m.nid = NOTIFYICONDATAW{
		HWnd:             hwnd,
		UID:              1,
		UFlags:           NIF_MESSAGE | NIF_TIP,
		UCallbackMessage: WM_TRAY_CALLBACK,
	}
	m.nid.CbSize = uint32(unsafe.Sizeof(m.nid))
	copy(m.nid.SzTip[:], utf16("TaskFlow Desktop"))
	pShellNotifyIcon.Call(NIM_ADD, uintptr(unsafe.Pointer(&m.nid)))

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

// SetTimerActive updates the tray tooltip based on timer state.
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
	m.nid.UFlags = NIF_TIP
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
			openBrowser("https://taskflow-ns.vercel.app")
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
