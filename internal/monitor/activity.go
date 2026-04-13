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
	ScreenshotWarningTime = 5 * time.Second
)

// NotifyFunc is a callback for showing notifications (e.g., tray balloon).
type NotifyFunc func(title, message string)

// ActivityMonitor tracks keyboard and mouse activity using platform-specific APIs.
type ActivityMonitor struct {
	mu            sync.Mutex
	apiClient     *api.Client
	appState      *state.AppState
	windowTracker *WindowTracker
	idleDetector  *IdleDetector
	inputTracker  *InputTracker
	screenshotCap *ScreenshotCapture
	onNotify      NotifyFunc

	// Current bucket counters (reset every 5 min)
	keyboardCount   int
	mouseCount      int
	activeSeconds   int
	idleSeconds     int
	appUsage map[string]int // app name → seconds in current bucket

	running bool
	cancel  context.CancelFunc // cancels all goroutines on Stop()
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
	}
}

// SetNotifyFunc sets the callback for showing notifications.
func (m *ActivityMonitor) SetNotifyFunc(fn NotifyFunc) {
	m.onNotify = fn
}

// Start begins activity monitoring. Safe to call multiple times — no-ops if already running.
func (m *ActivityMonitor) Start(parentCtx context.Context) {
	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return
	}
	m.running = true
	var ctx context.Context
	ctx, m.cancel = context.WithCancel(parentCtx)
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
	if m.cancel != nil {
		m.cancel()
	}
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

		case <-ticker.C:
			// Check idle state via Win32 GetLastInputInfo
			idleSec := m.idleDetector.GetIdleSeconds()
			m.appState.SetIdleSeconds(idleSec)

			// Read current input counters (keyboard/mouse event totals from the OS)
			kbCount, msCount := m.inputTracker.GetCounts()

			m.mu.Lock()
			// Calculate delta since last check (handles uint32 wrap-around)
			if lastKeyboard > 0 {
				var kbDelta, msDelta uint32
				if kbCount >= lastKeyboard {
					kbDelta = kbCount - lastKeyboard
				} else {
					kbDelta = (0xFFFFFFFF - lastKeyboard) + kbCount // wrap-around
				}
				if msCount >= lastMouse {
					msDelta = msCount - lastMouse
				} else {
					msDelta = (0xFFFFFFFF - lastMouse) + msCount
				}
				// Cap deltas to reasonable values (prevent spikes)
				if kbDelta < 1000 {
					m.keyboardCount += int(kbDelta)
				}
				if msDelta < 1000 {
					m.mouseCount += int(msDelta)
				}
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
				if len(m.appUsage) < maxAppsPerBucket || m.appUsage[appName] > 0 {
					m.appUsage[appName] += 5
				}
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

		case <-ticker.C:
			if !m.appState.IsTimerActive() {
				log.Println("Heartbeat skipped: timer not active")
				m.resetBucket()
				continue
			}
			m.sendCurrentBucket("")
		}
	}
}

// sendCurrentBucket sends the current activity bucket to the backend and resets.
// If screenshotURL is non-empty, it is attached to this bucket — screenshots
// flush the current bucket early so their counts reflect real accumulated
// activity rather than hardcoded zeros.
func (m *ActivityMonitor) sendCurrentBucket(screenshotURL string) {
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
	if screenshotURL != "" {
		bucket["screenshot_url"] = screenshotURL
	}

	m.resetBucketLocked()
	m.mu.Unlock()

	log.Printf("Sending heartbeat: kb=%d mouse=%d active=%ds idle=%ds app=%s screenshot=%v",
		bucket["keyboard_count"], bucket["mouse_count"], bucket["active_seconds"], bucket["idle_seconds"], topApp, screenshotURL != "")

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

const maxAppsPerBucket = 30

func (m *ActivityMonitor) resetBucketLocked() {
	m.keyboardCount = 0
	m.mouseCount = 0
	m.activeSeconds = 0
	m.idleSeconds = 0
	// Cap app usage map to prevent unbounded growth
	if len(m.appUsage) > maxAppsPerBucket {
		m.appUsage = make(map[string]int)
	} else {
		for k := range m.appUsage {
			delete(m.appUsage, k)
		}
	}
}

// captureScreenshots takes a screenshot every 10 minutes while the timer is active.
func (m *ActivityMonitor) captureScreenshots(ctx context.Context) {
	ticker := time.NewTicker(ScreenshotInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
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
// Has panic recovery to prevent screenshot failures from crashing the app.
func (m *ActivityMonitor) takeAndUploadScreenshot() {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("Screenshot capture recovered from panic: %v", r)
		}
	}()
	// 5-second warning via system tray balloon (reliable on all Windows versions)
	if m.onNotify != nil {
		m.onNotify("TaskFlow", "Screenshot in 5 seconds...")
	}
	time.Sleep(ScreenshotWarningTime)

	// Capture (CaptureScreen checks lock state internally)
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
		if m.onNotify != nil {
			m.onNotify("TaskFlow", "Screenshot upload failed — check your connection")
		}
		return
	}

	log.Printf("Screenshot uploaded: %s (%d KB)", cdnURL, len(jpegData)/1024)

	// Flush the current bucket early with the screenshot attached, so counts
	// reflect real accumulated activity instead of being hardcoded to zero.
	m.sendCurrentBucket(cdnURL)
}
