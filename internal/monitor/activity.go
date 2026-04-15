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
	keyboardCount int
	mouseCount    int
	activeSeconds int
	idleSeconds   int
	appUsage      map[string]int // app name → seconds in current bucket

	running bool
	cancel  context.CancelFunc // cancels all goroutines on Stop()
	wg      sync.WaitGroup     // joined by Stop so goroutines are fully drained
}

// sleepOrCancel blocks for d or returns early if ctx is cancelled.
// Used instead of time.Sleep in any goroutine that must honor shutdown.
func sleepOrCancel(ctx context.Context, d time.Duration) error {
	select {
	case <-time.After(d):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
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
	ctx, cancel := context.WithCancel(parentCtx)
	m.cancel = cancel
	m.running = true
	// Register all three goroutines BEFORE spawning so Stop cannot observe a
	// partially-populated WaitGroup.
	m.wg.Add(3)
	m.mu.Unlock()

	log.Println("Activity monitor started")

	go func() {
		defer m.wg.Done()
		defer func() {
			if r := recover(); r != nil {
				log.Printf("trackActivity recovered from panic: %v", r)
			}
		}()
		m.trackActivity(ctx)
	}()
	go func() {
		defer m.wg.Done()
		defer func() {
			if r := recover(); r != nil {
				log.Printf("captureScreenshots recovered from panic: %v", r)
			}
		}()
		m.captureScreenshots(ctx)
	}()
	go func() {
		defer m.wg.Done()
		defer func() {
			if r := recover(); r != nil {
				log.Printf("sendHeartbeats recovered from panic: %v", r)
			}
		}()
		m.sendHeartbeats(ctx)
	}()
}

// Stop halts activity monitoring and waits for all goroutines to exit.
// Safe to call multiple times — no-ops if already stopped.
//
// Before resetting the bucket, any pending activity data is flushed to
// the backend (H-MON-5). Without this, clicking SignOut or Logout would
// silently throw away up to HeartbeatInterval (5 min) of keyboard/mouse
// activity on the server side.
func (m *ActivityMonitor) Stop() {
	m.mu.Lock()
	if !m.running {
		m.mu.Unlock()
		return
	}
	m.running = false
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	m.mu.Unlock()

	// CRITICAL: drain goroutines before touching the bucket. Without
	// this, a rapid Stop→Start cycle (e.g. re-login) can leave old
	// goroutines running concurrently with new ones, corrupting counter
	// state.
	m.wg.Wait()

	// Flush whatever activity accumulated in the current partial bucket.
	// sendCurrentBucket resets the bucket as a side effect, so there is
	// no separate reset step.
	m.flushPendingLocked()

	log.Println("Activity monitor stopped")
}

// flushPendingLocked sends the current bucket synchronously if it has
// any data in it, then (via sendCurrentBucket's internal reset) leaves
// the bucket empty. The "Locked" suffix is nominal — this method takes
// the mutex itself, consistent with the rest of the flush path.
//
// If the backend rejects the heartbeat (e.g. session already closed
// server-side after SignOut), sendCurrentBucket logs the error and
// returns normally — we'd rather send one extra heartbeat that the
// backend ignores than drop real activity data on the floor.
func (m *ActivityMonitor) flushPendingLocked() {
	m.mu.Lock()
	hasData := m.keyboardCount > 0 || m.mouseCount > 0 ||
		m.activeSeconds > 0 || m.idleSeconds > 0 || len(m.appUsage) > 0
	m.mu.Unlock()
	if !hasData {
		return
	}
	m.sendCurrentBucket("")
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

	// Deep-copy appUsage before releasing the lock. The live m.appUsage map is
	// cleared in place by resetBucketLocked and mutated by trackActivity on the
	// next tick; embedding the raw reference into the bucket would corrupt the
	// JSON serialization of this heartbeat.
	topApp := ""
	topTime := 0
	appCopy := make(map[string]int, len(m.appUsage))
	for app, seconds := range m.appUsage {
		appCopy[app] = seconds
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
		"app_breakdown":  appCopy,
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
			m.takeAndUploadScreenshot(ctx)
		}
	}
}

// takeAndUploadScreenshot captures the screen, uploads to S3, and stores the URL.
// Has panic recovery to prevent screenshot failures from crashing the app.
// Honors ctx cancellation during the warning window so a Stop/Logout during
// the 5-second warning does NOT produce a post-stop capture (privacy contract).
func (m *ActivityMonitor) takeAndUploadScreenshot(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("Screenshot capture recovered from panic: %v", r)
		}
	}()
	// 5-second warning via system tray balloon (reliable on all Windows versions)
	if m.onNotify != nil {
		m.onNotify("TaskFlow", "Screenshot in 5 seconds...")
	}
	if err := sleepOrCancel(ctx, ScreenshotWarningTime); err != nil {
		// Monitor was stopped during the warning window — DO NOT capture.
		return
	}

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
