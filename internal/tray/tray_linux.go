//go:build linux

package tray

import (
	"fmt"
	"log"
	"os/exec"
	"sync"

	"github.com/godbus/dbus/v5"

	"taskflow-desktop/internal/config"
	"taskflow-desktop/internal/state"
)

// Manager manages the system tray icon on Linux.
//
// SCOPE: notification-only BY DESIGN. The Windows tray
// (tray_windows.go) renders a clickable system-tray icon with a Win32
// popup menu (Show Window / Stop Timer / Open Dashboard / Quit). The
// Linux equivalent would require a full StatusNotifierItem +
// com.canonical.dbusmenu D-Bus implementation (~700-1000 lines of
// protocol glue) — and every action that menu would offer already
// exists as a button in the React UI inside the app window. Linux
// users hit Stop Timer / Sign Out / Quit from there. We deliberately
// don't ship a redundant native menu.
//
// What this file DOES provide on Linux: notification balloons via
// org.freedesktop.Notifications. That's the same D-Bus service every
// notification daemon (mako, dunst, gnome-shell, plasma, xfce4-notifyd)
// implements — same service notify-send delegates to — so users see
// the standard notification bubble with zero extra packages. The
// Notify call previously shelled out to `notify-send`, which required
// libnotify-bin installed separately on every machine. Speaking D-Bus
// directly removes that dependency.
//
// stopCh is closed by Stop and selected on by Start. Without a dedicated
// stop channel, Stop just flipped m.running=false and the goroutine
// blocked on <-done (the app-lifetime channel) would leak if the app
// wanted to restart the tray without exiting. See H-TRAY-2.
type Manager struct {
	mu          sync.Mutex
	appState    *state.AppState
	handler     *ActionHandler
	running     bool
	timerActive bool
	statusText  string
	stopCh      chan struct{}

	// notifyOnce guards lazy connection to the session bus. A session
	// without D-Bus running (rare — typically headless) leaves
	// notifyObj nil and ShowBalloon degrades to a log line, matching
	// the old "notify-send not installed" failure mode.
	notifyOnce sync.Once
	notifyObj  dbus.BusObject
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

// Start runs the tray manager. On Linux, there's no message loop needed
// like Win32 — we just mark as running and block until the first of:
//   - the app-lifetime done channel closes (shutdown)
//   - Stop is called (closes stopCh)
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

	log.Println("System tray started (Linux)")

	select {
	case <-done:
	case <-stopCh:
	}

	m.mu.Lock()
	m.running = false
	m.stopCh = nil
	m.mu.Unlock()
	log.Println("System tray stopped (Linux)")
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
		// already closed
	default:
		close(m.stopCh)
	}
}

// notifier returns the cached D-Bus proxy for
// org.freedesktop.Notifications, or nil if the session bus is
// unavailable. Callers must tolerate nil.
func (m *Manager) notifier() dbus.BusObject {
	m.notifyOnce.Do(func() {
		conn, err := dbus.SessionBus()
		if err != nil {
			log.Printf("tray: no D-Bus session bus (%v) — notifications disabled", err)
			return
		}
		m.notifyObj = conn.Object(
			"org.freedesktop.Notifications",
			dbus.ObjectPath("/org/freedesktop/Notifications"),
		)
	})
	return m.notifyObj
}

// ShowBalloon displays a desktop notification via the
// org.freedesktop.Notifications D-Bus service — the same service every
// Linux notification daemon (dunst, mako, gnome-shell, plasma, xfce4-
// notifyd) implements. This is what notify-send does internally.
//
// If the session bus isn't reachable we log the message and return —
// same degrade-to-log contract the old exec("notify-send") had when
// libnotify-bin was missing.
func (m *Manager) ShowBalloon(title, message string) {
	if !m.running {
		log.Println("ShowBalloon skipped: tray not running")
		return
	}
	log.Printf("ShowBalloon: %s — %s", title, message)

	n := m.notifier()
	if n == nil {
		return
	}

	// Notify method signature (org.freedesktop.Notifications):
	//   app_name (s), replaces_id (u), app_icon (s), summary (s),
	//   body (s), actions (as), hints (a{sv}), expire_timeout (i)
	// See https://specifications.freedesktop.org/notification-spec/latest/
	call := n.Call(
		"org.freedesktop.Notifications.Notify", 0,
		"TaskFlow Desktop",
		uint32(0),
		"appointment-soon",
		title,
		message,
		[]string{},
		map[string]dbus.Variant{},
		int32(5000),
	)
	if call.Err != nil {
		log.Printf("tray: notify failed: %v", call.Err)
	}
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

// SetTimerStatus updates only the status text (tooltip / menu line)
// without changing the active flag. Linux tray manager uses
// statusText in the context-menu render path; the per-poll tooltip
// refresh just updates the cached value here so the next menu open
// shows current elapsed time.
func (m *Manager) SetTimerStatus(text string) {
	m.mu.Lock()
	m.statusText = text
	m.mu.Unlock()
}

// openBrowser opens a URL in the default browser. xdg-open is the
// XDG-standard entry point every major desktop (GNOME, KDE, XFCE, etc.)
// installs by default — replacing it would mean reimplementing mimeapps.
// list resolution, which is well out of scope here.
//
// Validated beforehand: xdg-open also routes by URI scheme and would
// happily dispatch file:/javascript:/custom-scheme: URIs to registered
// handlers. Defense-in-depth on top of config.Get's validation. See
// V2-M1.
func openBrowser(url string) {
	if !isSafeBrowserURL(url) {
		log.Printf("tray: refusing to open non-http(s) URL %q", url)
		return
	}
	if err := exec.Command("xdg-open", url).Start(); err != nil {
		log.Printf("tray: xdg-open failed: %v", err)
	}
}

// OpenDashboard opens the web dashboard in the default browser.
func (m *Manager) OpenDashboard() {
	openBrowser(config.Get().WebDashboardURL)
}
