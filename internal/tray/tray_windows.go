//go:build windows

package tray

import (
	"fmt"
	"log"
	"os"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
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

	// Standard Windows message IDs. Posting WM_CLOSE to our hidden tray
	// window lets DefWindowProc call DestroyWindow, which fires WM_DESTROY;
	// trayWndProc then calls PostQuitMessage(0) which puts WM_QUIT in the
	// thread's message queue. GetMessage sees WM_QUIT and returns 0, and
	// the tray message loop exits cleanly. This is the Win32-canonical
	// shutdown path — posting WM_QUIT directly to a hwnd is undefined.
	WM_DESTROY       = 0x0002
	WM_CLOSE         = 0x0010
	WM_APP           = 0x8000
	WM_TRAY_CALLBACK = WM_APP + 1
	// WM_APP_SHOW_BALLOON and WM_APP_SET_TIMER are custom codes posted
	// from arbitrary goroutines into the tray window's message queue,
	// so that the actual Shell_NotifyIcon call runs on the window-
	// owning thread (C-TRAY-1, C-TRAY-2). Win32 nominally requires
	// Shell_NotifyIcon to be called from the thread that created the
	// window; violating this produces silent races in practice.
	WM_APP_SHOW_BALLOON = WM_APP + 2
	WM_APP_SET_TIMER    = WM_APP + 3
	WM_COMMAND          = 0x0111
	WM_RBUTTONUP        = 0x0205
	WM_LBUTTONDBLCLK    = 0x0203

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

// Manager manages the system tray icon.
//
// All mutations to nid, timerActive, statusText, and pendingBalloon go
// through mu. SetTimerActive and ShowBalloon are called from arbitrary
// goroutines (activity monitor, Wails IPC bindings), but the actual
// Shell_NotifyIcon calls must happen on the window-owning thread —
// that's why both of those methods stash their intent under mu and
// post a WM_APP code to the message loop. See C-TRAY-1, C-TRAY-2.
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

	// stopped is closed by Start immediately after cleanup() returns.
	// Stop blocks on it so the caller — typically app.go's OnQuit
	// goroutine, which calls runtime.Quit right after — doesn't tear
	// the process down before NIM_DELETE has removed the tray icon.
	// Without this, Windows leaves a zombie icon in the system tray
	// until the user hovers the notification area and the OS sweeps
	// stale entries. Fixes "unable to close the app" / tray-ghost bug.
	stopped chan struct{}

	// pendingBalloon is set by ShowBalloon and consumed by the
	// WM_APP_SHOW_BALLOON handler inside trayWndProc. The handler
	// runs on the window-owning thread, so Shell_NotifyIcon is always
	// called from the correct thread (C-TRAY-2).
	pendingBalloon *pendingBalloonData
}

type pendingBalloonData struct {
	title   string
	message string
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

// globalManager lets the package-level trayWndProc callback reach the
// owning Manager instance. atomic.Pointer because Start writes it from
// whatever goroutine launched the tray while the Win32 callback thread
// reads it for every message dispatch — plain *Manager would be a
// data race the race detector flags immediately (H-TRAY-3).
var globalManager atomic.Pointer[Manager]

// Start initializes the system tray. Must be called from a goroutine.
func (m *Manager) Start(done <-chan struct{}) {
	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return
	}
	m.running = true
	m.stopped = make(chan struct{})
	globalManager.Store(m)
	m.mu.Unlock()

	// Close stopped on ANY exit from this function — including panics —
	// so Stop's Wait never hangs forever if something blows up during
	// window creation or the message loop.
	defer func() {
		m.mu.Lock()
		ch := m.stopped
		m.mu.Unlock()
		if ch != nil {
			select {
			case <-ch:
				// Already closed (Stop may have been called during
				// cleanup, depending on OS scheduling).
			default:
				close(ch)
			}
		}
	}()

	// Register window class
	className, _ := syscall.UTF16PtrFromString("TaskFlowTrayClass")
	wc := WNDCLASSEX{
		LpfnWndProc:   syscall.NewCallback(trayWndProc),
		LpszClassName: uintptr(unsafe.Pointer(className)),
	}
	wc.CbSize = uint32(unsafe.Sizeof(wc))
	pRegisterClass.Call(uintptr(unsafe.Pointer(&wc)))

	// Create hidden window for receiving tray messages
	windowName, _ := syscall.UTF16PtrFromString("TaskFlowTrayWindow")
	hwnd, _, _ := pCreateWindowEx.Call(
		0,                                       // dwExStyle
		uintptr(unsafe.Pointer(className)),       // lpClassName
		uintptr(unsafe.Pointer(windowName)),       // lpWindowName
		0,                                        // dwStyle (hidden)
		0, 0, 0, 0,                              // x, y, w, h
		0,                                        // hWndParent (desktop)
		0, 0, 0,                                  // hMenu, hInstance, lpParam
	)
	m.hwnd = hwnd
	log.Printf("Tray window created: hwnd=%v", hwnd)

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

	// Monitor the app-lifetime done channel. When the app shuts down, post
	// WM_CLOSE to the tray window; DefWindowProc → DestroyWindow →
	// WM_DESTROY → PostQuitMessage(0) → GetMessage returns 0 → loop exits.
	go func() {
		<-done
		pPostMessage.Call(m.hwnd, WM_CLOSE, 0, 0)
	}()

	// Message loop — must run on the same thread as CreateWindowEx
	var msg MSG
	for {
		ret, _, _ := pGetMessage.Call(
			uintptr(unsafe.Pointer(&msg)),
			0, 0, 0,
		)
		if ret == 0 || ret == uintptr(^uintptr(0)) { // WM_QUIT or error
			break
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		pDispatchMessage.Call(uintptr(unsafe.Pointer(&msg)))
	}

	m.cleanup()
}

func (m *Manager) cleanup() {
	pShellNotifyIcon.Call(NIM_DELETE, uintptr(unsafe.Pointer(&m.nid)))
	// Release the two HICON handles (base + active-with-dot overlay).
	// These are process-lifetime GDI handles — the OS will reclaim them
	// at exit anyway, but a future "restart tray without restarting
	// app" path would leak handles across every cycle without this.
	// DestroyIcon is safe on 0 handles (no-op).
	m.mu.Lock()
	if m.baseIcon != 0 {
		pDestroyIcon.Call(m.baseIcon)
		m.baseIcon = 0
	}
	if m.activeIcon != 0 {
		pDestroyIcon.Call(m.activeIcon)
		m.activeIcon = 0
	}
	m.running = false
	m.mu.Unlock()
	log.Println("System tray stopped")
}

// Stop asks the tray message loop to exit and BLOCKS until the loop
// has actually drained + cleanup() has removed the tray icon via
// NIM_DELETE. Idempotent and safe to call from any goroutine.
//
// Blocking is the key detail: the previous implementation just posted
// WM_CLOSE and returned immediately. The caller (app.go's OnQuit
// goroutine) would then call runtime.Quit, which tears the process
// down before the tray goroutine had a chance to NIM_DELETE. Windows
// leaves the orphaned icon in the system tray until the user hovers
// the notification area (stale-icon sweep). Users see "app closed but
// its tray icon is still there and unclickable."
//
// Bounded with a 3 s timeout so a wedged message loop can't prevent
// shutdown indefinitely. If we hit the timeout the icon may still
// ghost, but process teardown proceeds — better than hanging.
func (m *Manager) Stop() {
	m.mu.Lock()
	if !m.running || m.hwnd == 0 {
		m.mu.Unlock()
		return
	}
	hwnd := m.hwnd
	stopped := m.stopped
	m.mu.Unlock()

	pPostMessage.Call(hwnd, WM_CLOSE, 0, 0)

	if stopped == nil {
		return
	}
	select {
	case <-stopped:
		// Message loop exited + cleanup ran.
	case <-time.After(3 * time.Second):
		log.Println("tray: Stop timed out waiting for message loop; icon may ghost")
	}
}

// ShowBalloon displays a Windows notification balloon from the tray icon.
//
// Thread-safety (C-TRAY-2): Shell_NotifyIcon nominally requires the
// calling thread to be the one that created the tray window. This
// method is called from arbitrary goroutines (activity monitor during
// screenshots, auth flow, etc.), so we stash the pending balloon under
// m.mu and post a WM_APP_SHOW_BALLOON message; trayWndProc then calls
// Shell_NotifyIcon on the correct thread via handleShowBalloon.
func (m *Manager) ShowBalloon(title, message string) {
	m.mu.Lock()
	if !m.running || m.hwnd == 0 {
		m.mu.Unlock()
		log.Println("ShowBalloon skipped: tray not running")
		return
	}
	m.pendingBalloon = &pendingBalloonData{title: title, message: message}
	hwnd := m.hwnd
	m.mu.Unlock()

	log.Printf("ShowBalloon: %s - %s", title, message)
	pPostMessage.Call(hwnd, WM_APP_SHOW_BALLOON, 0, 0)
}

// handleShowBalloon runs on the window-owning thread (invoked from
// trayWndProc when it dispatches WM_APP_SHOW_BALLOON). Holds m.mu
// through the entire NOTIFYICONDATAW mutation and the Shell_NotifyIcon
// call so the Win32 struct is never observed half-written.
func (m *Manager) handleShowBalloon() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.running || m.pendingBalloon == nil {
		return
	}
	bal := m.pendingBalloon
	m.pendingBalloon = nil

	// Clear previous balloon text first
	for i := range m.nid.SzInfoTitle {
		m.nid.SzInfoTitle[i] = 0
	}
	for i := range m.nid.SzInfo {
		m.nid.SzInfo[i] = 0
	}

	// Windows caps title at 63 chars + null; message at 255 + null.
	// copy() silently truncates; log once so operators spot the
	// truncation during integration instead of debugging "why is my
	// long error message cut off?" See V3-M7.
	titleUTF16 := utf16(bal.title)
	msgUTF16 := utf16(bal.message)
	if len(titleUTF16) > len(m.nid.SzInfoTitle) {
		log.Printf("ShowBalloon: title truncated from %d to %d utf16 units", len(titleUTF16), len(m.nid.SzInfoTitle))
	}
	if len(msgUTF16) > len(m.nid.SzInfo) {
		log.Printf("ShowBalloon: message truncated from %d to %d utf16 units", len(msgUTF16), len(m.nid.SzInfo))
	}
	copy(m.nid.SzInfoTitle[:], titleUTF16)
	copy(m.nid.SzInfo[:], msgUTF16)
	m.nid.DwInfoFlags = 0x00000001 // NIIF_INFO
	// NIF_INFO is required for balloon; include NIF_ICON + NIF_TIP to keep icon visible
	m.nid.UFlags = NIF_INFO | NIF_ICON | NIF_TIP
	ret, _, err := pShellNotifyIcon.Call(NIM_MODIFY, uintptr(unsafe.Pointer(&m.nid)))
	log.Printf("ShowBalloon result: ret=%d err=%v", ret, err)
}

// SetTimerActive updates the tray icon and tooltip based on timer state.
//
// Thread-safety (C-TRAY-1): stashes the requested state under m.mu and
// posts a WM_APP_SET_TIMER message; handleSetTimer then performs the
// Win32 struct mutation and Shell_NotifyIcon call on the window-owning
// thread with m.mu held, so the 4 KB NOTIFYICONDATAW struct is never
// read by the message loop mid-mutation.
func (m *Manager) SetTimerActive(active bool, task *state.CurrentTask) {
	m.mu.Lock()
	m.timerActive = active
	if active && task != nil {
		m.statusText = fmt.Sprintf("Working: %s", task.TaskTitle)
	} else {
		m.statusText = "Timer stopped"
	}
	if !m.running || m.hwnd == 0 {
		m.mu.Unlock()
		return
	}
	hwnd := m.hwnd
	m.mu.Unlock()

	pPostMessage.Call(hwnd, WM_APP_SET_TIMER, 0, 0)
}

// SetTimerStatus updates ONLY the tooltip text (status line + the
// elapsed-time substring shown next to the running task). Doesn't
// touch the icon — used by the per-poll tooltip-refresh path so the
// tray shows "Auth refactor — 1h 23m" while the user has the window
// hidden, without the icon flicker that a full SetTimerActive call
// would cause.
func (m *Manager) SetTimerStatus(text string) {
	m.mu.Lock()
	m.statusText = text
	if !m.running || m.hwnd == 0 {
		m.mu.Unlock()
		return
	}
	hwnd := m.hwnd
	m.mu.Unlock()
	pPostMessage.Call(hwnd, WM_APP_SET_TIMER, 0, 0)
}

// handleSetTimer runs on the window-owning thread. Holds m.mu through
// the full NOTIFYICONDATAW mutation and the Shell_NotifyIcon call.
func (m *Manager) handleSetTimer() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.running {
		return
	}

	tip := fmt.Sprintf("TaskFlow — %s", m.statusText)
	if len(tip) > 127 {
		tip = tip[:127]
	}
	// Clear previous tip text before copying to avoid leftover bytes.
	for i := range m.nid.SzTip {
		m.nid.SzTip[i] = 0
	}
	copy(m.nid.SzTip[:], utf16(tip))

	// Swap icon: green dot when active, normal when stopped
	if m.timerActive && m.activeIcon != 0 {
		m.nid.HIcon = m.activeIcon
	} else if m.baseIcon != 0 {
		m.nid.HIcon = m.baseIcon
	}

	m.nid.UFlags = NIF_TIP | NIF_ICON
	pShellNotifyIcon.Call(NIM_MODIFY, uintptr(unsafe.Pointer(&m.nid)))
}

// trayWndProc handles window messages for the tray icon.
func trayWndProc(hwnd, msg, wParam, lParam uintptr) uintptr {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("Recovered panic in trayWndProc: %v", r)
		}
	}()

	// Load the Manager pointer once per message dispatch. Subsequent
	// calls during this one dispatch see a consistent view.
	mgr := globalManager.Load()

	switch msg {
	case WM_DESTROY:
		// WM_DESTROY arrives after DefWindowProc(WM_CLOSE) calls
		// DestroyWindow. We translate it into WM_QUIT so that the
		// message loop's GetMessage returns 0 and Start() can return.
		// Without this the loop hangs forever, leaving a zombie process
		// visible in Task Manager after the user clicks Quit.
		pPostQuitMessage.Call(0)
		return 0

	case WM_APP_SHOW_BALLOON:
		// Delivered from ShowBalloon (any goroutine) so that the actual
		// Shell_NotifyIcon(NIM_MODIFY) call runs on the window-owning
		// thread. Without this, Win32 nominally-illegal cross-thread
		// Shell_NotifyIcon calls produce silent races. See C-TRAY-2.
		if mgr != nil {
			mgr.handleShowBalloon()
		}
		return 0

	case WM_APP_SET_TIMER:
		// Delivered from SetTimerActive. Same rationale as the balloon
		// path — the Shell_NotifyIcon call runs on this thread only.
		// See C-TRAY-1.
		if mgr != nil {
			mgr.handleSetTimer()
		}
		return 0

	case WM_TRAY_CALLBACK:
		// With NOTIFYICON_VERSION_4, the event is in the low word of lParam
		event := lParam & 0xFFFF
		switch event {
		case WM_RBUTTONUP:
			showContextMenu(hwnd)
		case WM_LBUTTONDBLCLK:
			if mgr != nil && mgr.handler != nil && mgr.handler.OnShowWindow != nil {
				log.Println("Tray: double-click → ShowWindow")
				go mgr.handler.OnShowWindow()
			}
		case 0x0201: // WM_LBUTTONDOWN — single click also shows window
			if mgr != nil && mgr.handler != nil && mgr.handler.OnShowWindow != nil {
				log.Println("Tray: single-click → ShowWindow")
				go mgr.handler.OnShowWindow()
			}
		}
		return 0

	case WM_COMMAND:
		id := wParam & 0xFFFF
		switch id {
		case IDM_SHOW:
			if mgr != nil && mgr.handler != nil && mgr.handler.OnShowWindow != nil {
				go mgr.handler.OnShowWindow()
			}
		case IDM_STOP:
			if mgr != nil && mgr.handler != nil && mgr.handler.OnStopTimer != nil {
				// Dispatch on a goroutine — OnStopTimer invokes
				// SignOut → ActivityMonitor.Stop →
				// TrayManager.SetTimerActive, and the last call
				// blocks on m.mu which the message loop also
				// needs. Running synchronously here deadlocks
				// the tray. OnShowWindow and OnQuit already use
				// this pattern; OnStopTimer was the outlier.
				// See V3-H6.
				go mgr.handler.OnStopTimer()
			}
		case IDM_DASHBOARD:
			openBrowser(config.Get().WebDashboardURL)
		case IDM_QUIT:
			if mgr != nil && mgr.handler != nil && mgr.handler.OnQuit != nil {
				go mgr.handler.OnQuit()
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
	if mgr := globalManager.Load(); mgr != nil {
		mgr.mu.Lock()
		status = mgr.statusText
		isActive := mgr.timerActive
		mgr.mu.Unlock()

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
	// ShellExecuteW routes arbitrary URI schemes to registered
	// handlers (file:, javascript:, custom-protocol://…). Defense-in-
	// depth on top of config.Get's validation. See V2-M1.
	if !isSafeBrowserURL(url) {
		log.Printf("tray: refusing to open non-http(s) URL %q", url)
		return
	}
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

	// Delete bitmaps after icon is created — CreateIconIndirect copies them
	pDeleteObject.Call(hbmMask)
	pDeleteObject.Call(hbmColor)

	return newIcon
}
