//go:build darwin

package tray

import (
	"fmt"
	"log"
	"os/exec"
	"strings"
	"sync"

	"taskflow-desktop/internal/config"
	"taskflow-desktop/internal/state"
)

// Manager manages notifications on macOS.
//
// SCOPE: notification-only BY DESIGN. The Windows tray
// (tray_windows.go) renders a clickable system-tray icon with a Win32
// popup menu (Show Window / Stop Timer / Open Dashboard / Quit). The
// macOS equivalent would require CGo bindings to NSStatusItem + NSMenu
// (~250 lines of Go + ~150 lines of Objective-C) plus dispatch_async
// coordination because Wails owns the NSApplication main loop — and
// every action that menu would offer already exists as a button in
// the React UI inside the app window. macOS users hit Stop Timer /
// Sign Out / Quit from there. We deliberately don't ship a redundant
// native menu bar item.
//
// What this file DOES provide on macOS: native notifications via
// osascript's `display notification` AppleScript primitive, with
// argument-passing (not string interpolation) so server-controlled
// title/message text can never inject AppleScript. See C-TRAY-3.
//
// stopCh is closed by Stop and selected on by Start, matching the Linux
// tray's shape. See H-TRAY-2.
type Manager struct {
	mu          sync.Mutex
	appState    *state.AppState
	handler     *ActionHandler
	running     bool
	timerActive bool
	statusText  string
	stopCh      chan struct{}
}

func NewManager(appState *state.AppState) *Manager {
	return &Manager{
		appState:   appState,
		statusText: "Timer stopped",
	}
}

func (m *Manager) SetHandler(h *ActionHandler) {
	m.handler = h
}

// Start runs the tray manager. On macOS, the Wails framework owns the
// NSApplication main loop, so we don't create our own. We just mark as
// running and block until the first of the app-lifetime done channel
// or our own stopCh fires.
func (m *Manager) Start(done <-chan struct{}) {
	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return
	}
	m.running = true
	m.stopCh = make(chan struct{})
	stopCh := m.stopCh
	m.mu.Unlock()

	log.Println("System tray started (macOS)")

	select {
	case <-done:
	case <-stopCh:
	}

	m.mu.Lock()
	m.running = false
	m.stopCh = nil
	m.mu.Unlock()
	log.Println("System tray stopped (macOS)")
}

// Stop signals the tray goroutine to exit. Safe to call from any
// goroutine and idempotent.
func (m *Manager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.running || m.stopCh == nil {
		return
	}
	select {
	case <-m.stopCh:
	default:
		close(m.stopCh)
	}
}

// ShowBalloon displays a macOS notification using osascript.
//
// Title and message originate from server-controlled data (e.g. task titles
// from the backend). They MUST NOT be string-interpolated into the
// AppleScript source — doing that would let a compromised backend inject
// arbitrary shell via crafted `"` characters (C-TRAY-3). Instead we pass the
// AppleScript as a fixed program that reads arguments from `on run argv`,
// with the untrusted strings handed in after `--` as pure data.
func (m *Manager) ShowBalloon(title, message string) {
	if !m.running {
		log.Println("ShowBalloon skipped: tray not running")
		return
	}
	log.Printf("ShowBalloon: %s — %s", title, message)

	cmd := exec.Command("osascript",
		"-e", "on run argv",
		"-e", "display notification (item 1 of argv) with title (item 2 of argv)",
		"-e", "end run",
		"--",
		sanitizeNotificationText(message),
		sanitizeNotificationText(title),
	)
	if err := cmd.Start(); err != nil {
		log.Printf("osascript launch failed: %v", err)
	}
}

// sanitizeNotificationText removes control characters that could disrupt the
// AppleScript `on run argv` path (the data path is already injection-safe,
// but a stray NUL or newline would produce confusing notifications).
func sanitizeNotificationText(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 32 && r != '\t' {
			return -1
		}
		return r
	}, s)
}

// SetTimerActive updates tray state based on timer status.
func (m *Manager) SetTimerActive(active bool, task *state.CurrentTask) {
	m.mu.Lock()
	m.timerActive = active
	if active && task != nil {
		m.statusText = fmt.Sprintf("Working: %s", task.TaskTitle)
	} else {
		m.statusText = "Timer stopped"
	}
	m.mu.Unlock()
}

// openBrowser opens a URL in the default browser. Validated
// beforehand because macOS `open` routes by URI scheme to whatever
// handler is registered (javascript:, file:, custom-protocol:// …) —
// a misconfigured or malicious dashboard URL must not be able to
// launch arbitrary handlers. See V2-M1.
func openBrowser(url string) {
	if !isSafeBrowserURL(url) {
		log.Printf("tray: refusing to open non-http(s) URL %q", url)
		return
	}
	if err := exec.Command("open", url).Start(); err != nil {
		log.Printf("tray: open failed: %v", err)
	}
}

// OpenDashboard opens the web dashboard in the default browser.
func (m *Manager) OpenDashboard() {
	openBrowser(config.Get().WebDashboardURL)
}
