package monitor

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"taskflow-desktop/internal/api"
	"taskflow-desktop/internal/state"
)

const (
	// HeartbeatInterval is how often activity data is sent to the backend.
	HeartbeatInterval = 5 * time.Minute

	// ScreenshotInterval is how often screenshots are taken.
	ScreenshotInterval = 10 * time.Minute

	// ScreenshotWarningTime is the notification shown before capture.
	ScreenshotWarningTime = 3 * time.Second
)

// ActivityBucket holds aggregated activity data for a 5-minute window.
type ActivityBucket struct {
	Timestamp     string         `json:"timestamp"`
	KeyboardCount int            `json:"keyboardCount"`
	MouseCount    int            `json:"mouseCount"`
	ActiveSeconds int            `json:"activeSeconds"`
	IdleSeconds   int            `json:"idleSeconds"`
	TopApp        string         `json:"topApp"`
	AppBreakdown  map[string]int `json:"appBreakdown"` // app name → seconds
}

// ActivityMonitor tracks keyboard and mouse activity using Win32 APIs.
type ActivityMonitor struct {
	mu            sync.Mutex
	apiClient     *api.Client
	appState      *state.AppState
	windowTracker *WindowTracker
	idleDetector  *IdleDetector
	inputTracker  *InputTracker
	screenshotCap *ScreenshotCapture

	// Current bucket counters (reset every 5 min)
	keyboardCount   int
	mouseCount      int
	activeSeconds   int
	idleSeconds     int
	appUsage        map[string]int // app name → seconds in current bucket
	lastScreenshot  string         // CDN URL of last screenshot taken

	running  bool
	stopChan chan struct{}
}

// NewActivityMonitor creates a new activity monitor.
func NewActivityMonitor(apiClient *api.Client, appState *state.AppState) *ActivityMonitor {
	return &ActivityMonitor{
		apiClient:     apiClient,
		appState:      appState,
		windowTracker: NewWindowTracker(),
		idleDetector:  NewIdleDetector(),
		inputTracker:  NewInputTracker(),
		screenshotCap: NewScreenshotCapture(),
		appUsage:      make(map[string]int),
		stopChan:      make(chan struct{}),
	}
}

// Start begins activity monitoring. Safe to call multiple times — no-ops if already running.
func (m *ActivityMonitor) Start(ctx context.Context) {
	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return
	}
	m.running = true
	m.stopChan = make(chan struct{}) // Fresh channel for this run
	m.mu.Unlock()

	log.Println("Activity monitor started")

	go m.trackActivity(ctx)
	go m.captureScreenshots(ctx)
	go m.sendHeartbeats(ctx)
}

// Stop halts activity monitoring. Safe to call multiple times — no-ops if already stopped.
func (m *ActivityMonitor) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.running {
		return
	}

	m.running = false
	close(m.stopChan)
	m.resetBucketLocked()
	log.Println("Activity monitor stopped")
}

// trackActivity runs every second to track idle state, input counts, and active window.
func (m *ActivityMonitor) trackActivity(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	// Track active window every 5 seconds
	windowTicker := time.NewTicker(5 * time.Second)
	defer windowTicker.Stop()

	var lastKeyboard, lastMouse uint32

	for {
		select {
		case <-ctx.Done():
			return
		case <-m.stopChan:
			return

		case <-ticker.C:
			// Check idle state via Win32 GetLastInputInfo
			idleSec := m.idleDetector.GetIdleSeconds()
			m.appState.SetIdleSeconds(idleSec)

			// Read current input counters (keyboard/mouse event totals from the OS)
			kbCount, msCount := m.inputTracker.GetCounts()

			m.mu.Lock()
			// Calculate delta since last check
			if lastKeyboard > 0 {
				kbDelta := kbCount - lastKeyboard
				msDelta := msCount - lastMouse
				m.keyboardCount += int(kbDelta)
				m.mouseCount += int(msDelta)
			}
			lastKeyboard = kbCount
			lastMouse = msCount

			if idleSec > 2 {
				m.idleSeconds++
			} else {
				m.activeSeconds++
			}
			m.mu.Unlock()

		case <-windowTicker.C:
			appName := m.windowTracker.GetActiveWindowApp()
			if appName != "" {
				m.mu.Lock()
				m.appUsage[appName] += 5
				m.mu.Unlock()
			}
		}
	}
}

// sendHeartbeats sends activity data to the backend every 5 minutes.
func (m *ActivityMonitor) sendHeartbeats(ctx context.Context) {
	ticker := time.NewTicker(HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-m.stopChan:
			return

		case <-ticker.C:
			if !m.appState.IsTimerActive() {
				m.resetBucket()
				continue
			}
			m.sendCurrentBucket()
		}
	}
}

// sendCurrentBucket sends the current activity bucket to the backend and resets.
func (m *ActivityMonitor) sendCurrentBucket() {
	m.mu.Lock()

	topApp := ""
	topTime := 0
	for app, seconds := range m.appUsage {
		if seconds > topTime {
			topApp = app
			topTime = seconds
		}
	}

	bucket := map[string]interface{}{
		"timestamp":      time.Now().UTC().Format(time.RFC3339),
		"keyboard_count": m.keyboardCount,
		"mouse_count":    m.mouseCount,
		"active_seconds": m.activeSeconds,
		"idle_seconds":   m.idleSeconds,
		"top_app":        topApp,
		"app_breakdown":  m.appUsage,
	}
	if m.lastScreenshot != "" {
		bucket["screenshot_url"] = m.lastScreenshot
	}

	m.resetBucketLocked()
	m.mu.Unlock()

	log.Printf("Sending heartbeat: kb=%d mouse=%d active=%ds idle=%ds app=%s",
		bucket["keyboard_count"], bucket["mouse_count"], bucket["active_seconds"], bucket["idle_seconds"], topApp)

	if err := m.apiClient.SendActivityHeartbeat(bucket); err != nil {
		log.Printf("Failed to send activity heartbeat: %v", err)
	} else {
		log.Println("Heartbeat sent successfully")
	}
}

func (m *ActivityMonitor) resetBucket() {
	m.mu.Lock()
	m.resetBucketLocked()
	m.mu.Unlock()
}

func (m *ActivityMonitor) resetBucketLocked() {
	m.keyboardCount = 0
	m.mouseCount = 0
	m.activeSeconds = 0
	m.idleSeconds = 0
	m.appUsage = make(map[string]int)
	m.lastScreenshot = ""
}

// captureScreenshots takes a screenshot every 10 minutes while the timer is active.
func (m *ActivityMonitor) captureScreenshots(ctx context.Context) {
	ticker := time.NewTicker(ScreenshotInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-m.stopChan:
			return
		case <-ticker.C:
			if !m.appState.IsTimerActive() {
				continue
			}
			m.takeAndUploadScreenshot()
		}
	}
}

// takeAndUploadScreenshot captures the screen, uploads to S3, and stores the URL.
func (m *ActivityMonitor) takeAndUploadScreenshot() {
	// 3-second warning
	ShowNotification("TaskFlow", "Screenshot in 3 seconds...")
	time.Sleep(ScreenshotWarningTime)

	// Skip if screen is locked
	if m.screenshotCap.IsScreenLocked() {
		log.Println("Screenshot skipped: screen is locked")
		return
	}

	// Capture
	jpegData, err := m.screenshotCap.CaptureScreenDefault()
	if err != nil {
		log.Printf("Screenshot capture failed: %v", err)
		return
	}

	// Generate filename with timestamp
	filename := fmt.Sprintf("screenshot_%s.jpg", time.Now().UTC().Format("20060102_150405"))

	// Upload to S3
	cdnURL, err := m.apiClient.UploadScreenshot(jpegData, filename)
	if err != nil {
		log.Printf("Screenshot upload failed: %v", err)
		return
	}

	// Store URL for next heartbeat
	m.mu.Lock()
	m.lastScreenshot = cdnURL
	m.mu.Unlock()

	log.Printf("Screenshot uploaded: %s (%d KB)", cdnURL, len(jpegData)/1024)
}
