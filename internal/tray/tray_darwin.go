//go:build darwin

package tray

import (
	"fmt"
	"log"
	"os/exec"
	"sync"

	"taskflow-desktop/internal/config"
	"taskflow-desktop/internal/state"
)

// Manager manages the menu bar status item on macOS.
// Uses osascript for notifications. Full NSStatusItem via CGo can be added
// for a native menu bar icon with dropdown menu.
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

// Start runs the tray manager. On macOS, the Wails framework owns the
// NSApplication main loop, so we don't create our own. We just mark as
// running and block until done.
func (m *Manager) Start(done <-chan struct{}) {
	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return
	}
	m.running = true
	m.mu.Unlock()

	log.Println("System tray started (macOS)")

	// Block until shutdown signal
	<-done

	m.mu.Lock()
	m.running = false
	m.mu.Unlock()
	log.Println("System tray stopped (macOS)")
}

func (m *Manager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.running = false
}

// ShowBalloon displays a macOS notification using osascript.
func (m *Manager) ShowBalloon(title, message string) {
	if !m.running {
		log.Println("ShowBalloon skipped: tray not running")
		return
	}
	log.Printf("ShowBalloon: %s — %s", title, message)

	exec.Command("osascript", "-e",
		fmt.Sprintf(`display notification "%s" with title "%s"`, message, title),
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
	exec.Command("open", url).Start()
}

// OpenDashboard opens the web dashboard in the default browser.
func (m *Manager) OpenDashboard() {
	openBrowser(config.Get().WebDashboardURL)
}
