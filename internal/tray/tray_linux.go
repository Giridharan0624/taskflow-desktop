//go:build linux

package tray

import (
	"fmt"
	"log"
	"os/exec"
	"sync"

	"taskflow-desktop/internal/config"
	"taskflow-desktop/internal/state"
)

// Manager manages the system tray icon on Linux.
// Uses notify-send for notifications and a background goroutine for state.
// Full D-Bus StatusNotifierItem implementation can be added for richer tray support.
type Manager struct {
	mu          sync.Mutex
	appState    *state.AppState
	handler     *ActionHandler
	running     bool
	timerActive bool
	statusText  string
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
// like Win32 — we just mark as running and block until done.
func (m *Manager) Start(done <-chan struct{}) {
	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return
	}
	m.running = true
	m.mu.Unlock()

	log.Println("System tray started (Linux)")

	// Block until shutdown signal
	<-done

	m.mu.Lock()
	m.running = false
	m.mu.Unlock()
	log.Println("System tray stopped (Linux)")
}

func (m *Manager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.running = false
}

// ShowBalloon displays a desktop notification using notify-send.
func (m *Manager) ShowBalloon(title, message string) {
	if !m.running {
		log.Println("ShowBalloon skipped: tray not running")
		return
	}
	log.Printf("ShowBalloon: %s — %s", title, message)

	// notify-send is available on most Linux desktops (GNOME, KDE, XFCE)
	exec.Command("notify-send", title, message,
		"-t", "5000",                  // 5 second timeout
		"-a", "TaskFlow Desktop",      // app name
		"-i", "appointment-soon",      // icon
	).Start()
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

// openBrowser opens a URL in the default browser.
func openBrowser(url string) {
	exec.Command("xdg-open", url).Start()
}

// OpenDashboard opens the web dashboard in the default browser.
func (m *Manager) OpenDashboard() {
	openBrowser(config.Get().WebDashboardURL)
}
