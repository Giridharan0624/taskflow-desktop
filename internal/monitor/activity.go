package monitor

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"sync"
	"time"

	"taskflow-desktop/internal/api"
	"taskflow-desktop/internal/queue"
	"taskflow-desktop/internal/state"
)

const (
	// HeartbeatInterval is how often activity data is sent to the backend.
	HeartbeatInterval = 5 * time.Minute

	// ScreenshotMinInterval / ScreenshotMaxInterval bracket the
	// random window between captures: every screenshot fires at a
	// uniformly-chosen moment in [9 min, 10 min) since the last
	// one. The randomness defeats trivial mouse-jiggler / "hide
	// the bad app at minute :09:55" patterns without changing the
	// long-run average rate (~1 capture per 9.5 min).
	ScreenshotMinInterval = 9 * time.Minute
	ScreenshotMaxInterval = 10 * time.Minute
)

// nextScreenshotDelay returns a random duration uniformly distributed
// in [ScreenshotMinInterval, ScreenshotMaxInterval). Uses math/rand
// (no crypto needed — this is anti-evasion jitter, not a security
// primitive). math/rand is auto-seeded since Go 1.20, so we don't
// need an explicit Seed call.
func nextScreenshotDelay() time.Duration {
	span := int64(ScreenshotMaxInterval - ScreenshotMinInterval)
	return ScreenshotMinInterval + time.Duration(rand.Int63n(span))
}

// ActivityMonitor tracks keyboard and mouse activity using platform-specific APIs.
type ActivityMonitor struct {
	mu            sync.Mutex
	apiClient     *api.Client
	appState      *state.AppState
	windowTracker *WindowTracker
	idleDetector  *IdleDetector
	inputTracker  *InputTracker
	screenshotCap *ScreenshotCapture

	// Offline-resilience queues (best-effort — if their constructor
	// errors at startup we log and proceed without persistence; the
	// app still works, just falls back to the old lose-on-failure
	// behavior). See V3-offline.
	hbQueue   *queue.HeartbeatQueue
	ssQueue   *queue.ScreenshotQueue

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
	m := &ActivityMonitor{
		apiClient:     apiClient,
		appState:      appState,
		windowTracker: NewWindowTracker(),
		idleDetector:  NewIdleDetector(),
		inputTracker:  NewInputTracker(),
		screenshotCap: NewScreenshotCapture(),
		appUsage:      make(map[string]int),
	}
	// Best-effort queue initialization. A failure here means we can't
	// persist across network outages / process restarts, but the app
	// is still useful — fall back to old "send now or lose" behavior.
	if hb, err := queue.NewHeartbeatQueue(); err != nil {
		log.Printf("activity: heartbeat queue disabled (%v) — offline resilience degraded", err)
	} else {
		m.hbQueue = hb
	}
	if ss, err := queue.NewScreenshotQueue(); err != nil {
		log.Printf("activity: screenshot queue disabled (%v) — offline resilience degraded", err)
	} else {
		m.ssQueue = ss
	}
	return m
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
	// Register all goroutines BEFORE spawning so Stop cannot observe a
	// partially-populated WaitGroup.
	m.wg.Add(4)
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
	// Drain worker: retries queued heartbeats + screenshots every
	// 30 s so an offline-then-online transition catches up without
	// waiting for a new heartbeat tick. See V3-offline.
	go func() {
		defer m.wg.Done()
		defer func() {
			if r := recover(); r != nil {
				log.Printf("drainWorker recovered from panic: %v", r)
			}
		}()
		m.drainWorker(ctx)
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

	// Zero the input tracker totals + reseed its baselines. Without
	// this, the atomic keyboard/mouse counters retain the full
	// historical totals across Start→Stop→Start cycles, and the first
	// heartbeat after re-login reports a huge spurious delta that the
	// <1000 spike cap silently truncates (dropping legitimate bursts
	// along with the bogus historical portion). See M-MON-1.
	if m.inputTracker != nil {
		m.inputTracker.Reset()
	}

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
	// Keep the lock held across the "has data?" decision. Previously
	// this released the lock, then re-entered sendCurrentBucket which
	// re-acquired it — during the gap a concurrent path could have
	// reset the bucket, leading to a spurious 0/0/0 heartbeat.
	// Stop() has already drained goroutines by the time we get here,
	// so this path is guaranteed to run single-threaded — but holding
	// the lock eliminates the fragility if that assumption ever
	// slips. See V3-M3.
	m.mu.Lock()
	hasData := m.keyboardCount > 0 || m.mouseCount > 0 ||
		m.activeSeconds > 0 || m.idleSeconds > 0 || len(m.appUsage) > 0
	if !hasData {
		m.mu.Unlock()
		return
	}
	// sendCurrentBucketLocked expects m.mu held and releases it
	// internally after snapshotting, before issuing the HTTP call.
	m.sendCurrentBucketLocked("")
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
//
// Captures the timer state once at the top of each tick; if it flips
// to inactive between this check and the HTTP call (user stopped the
// timer, logout, session expired), the in-flight heartbeat will land
// but the backend rejects it cleanly via session-id validation.
// Clearer than re-checking inside sendCurrentBucket, and matches the
// screenshot path. See V3-M8.
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
			// Per-tenant feature gate. When the OWNER turns
			// `activity_monitoring` off in /settings/organization →
			// Features, suppress the heartbeat. Mirrors the
			// ScreenshotsEnabled() gate on the screenshot loop, and
			// matches the backend's require_feature("activity_monitoring")
			// guard on POST /activity/heartbeat — checking here avoids
			// the round-trip + spurious 403 logs.
			if !m.apiClient.ActivityMonitoringEnabled() {
				log.Println("Heartbeat skipped: activity_monitoring disabled")
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
	m.sendCurrentBucketLocked(screenshotURL)
}

// snapshotBucketLocked builds the heartbeat bucket from current
// in-memory counters and resets them. Caller MUST hold m.mu and is
// responsible for unlocking — this helper releases the lock after the
// snapshot+reset so the live counters can resume accumulating into a
// fresh bucket. Returns the bucket so the caller can decide whether to
// enqueue it to hbQueue, attach to a screenshot retry, or drop it.
//
// Split out from sendCurrentBucketLocked so the screenshot-retry path
// can capture the bucket WITHOUT going to the heartbeat queue — the
// bucket gets pinned to the queued JPEG instead, then fired together
// once the upload retry succeeds. Closes the screenshot ↔ activity-
// counts orphan window from the desktop audit.
func (m *ActivityMonitor) snapshotBucketLocked(screenshotURL string) map[string]interface{} {
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
	return bucket
}

// sendCurrentBucketLocked is the lock-holding variant. Caller MUST
// hold m.mu; this function releases it internally after snapshotting
// the bucket, then issues the HTTP call with no lock held (we must
// never block I/O under the mutex). Used by flushPendingLocked to
// avoid the lock-drop-relock race window. See V3-M3.
func (m *ActivityMonitor) sendCurrentBucketLocked(screenshotURL string) {
	bucket := m.snapshotBucketLocked(screenshotURL)

	log.Printf("Enqueue heartbeat: kb=%d mouse=%d active=%ds idle=%ds app=%s screenshot=%v",
		bucket["keyboard_count"], bucket["mouse_count"], bucket["active_seconds"], bucket["idle_seconds"], bucket["top_app"], screenshotURL != "")

	// Persist first, send after. This is the offline-resilience
	// contract: the on-disk queue is the source of truth. If the
	// app is killed / network drops / backend is down, the bucket
	// survives and gets replayed on the next drain tick.
	if m.hbQueue != nil {
		if err := m.hbQueue.Enqueue(bucket); err != nil {
			log.Printf("Failed to enqueue heartbeat (falling back to send-once): %v", err)
			m.trySendOnce(bucket)
			return
		}
		// Kick off a drain attempt immediately — if the network is
		// fine we drain within one tick and the entry is gone.
		m.drainHeartbeats(context.Background())
		return
	}

	// Fallback path: queue unavailable, use the old lose-on-failure
	// semantics.
	m.trySendOnce(bucket)
}

// trySendOnce is the legacy send-and-log path used only when the
// persistent queue is unavailable (startup failed to create it).
// Keeps the auth-aware stop behavior from V3-M5.
func (m *ActivityMonitor) trySendOnce(bucket map[string]interface{}) {
	if err := m.apiClient.SendActivityHeartbeat(bucket); err != nil {
		if errors.Is(err, api.ErrNotAuthenticated) {
			log.Println("Heartbeat aborted: not authenticated — stopping monitor")
			go m.Stop()
			return
		}
		log.Printf("Failed to send activity heartbeat: %v", err)
	} else {
		log.Println("Heartbeat sent successfully")
	}
}

// drainHeartbeats replays queued heartbeats to the backend in FIFO
// order. Stops on the first persistent failure so a dead network
// doesn't hot-loop us. Safe to call opportunistically on every
// enqueue AND on a timer — the queue's mutex serializes.
func (m *ActivityMonitor) drainHeartbeats(ctx context.Context) {
	if m.hbQueue == nil {
		return
	}
	sent := m.hbQueue.Drain(ctx, func(bucket map[string]interface{}) error {
		err := m.apiClient.SendActivityHeartbeat(bucket)
		if err != nil {
			// Not-authenticated is terminal — don't replay, just
			// stop the monitor. The queue keeps the entry for
			// when the user logs in again.
			if errors.Is(err, api.ErrNotAuthenticated) {
				log.Println("Heartbeat drain aborted: not authenticated — stopping monitor")
				go m.Stop()
				return err
			}
		}
		return err
	})
	if sent > 0 {
		log.Printf("Heartbeat drain: %d bucket(s) sent (pending=%d)", sent, m.hbQueue.Count())
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

// captureScreenshots takes a screenshot at a random moment between
// 9 and 10 minutes after the previous one (or after Start). Every
// capture re-rolls the next delay so the user can't predict the
// timing pattern — defeats mouse-jiggler / "alt-tab to clean
// window at exactly minute :10" evasion while keeping the long-
// run average close to once per 9.5 minutes.
//
// Phase 6: per-tenant feature gate. ScreenshotsEnabled() reads the
// cached OrgSettings; on a tenant where features.screenshots is
// false (or unknown — fail-closed) the tick is a no-op. The
// goroutine itself stays alive so that toggling the setting later
// flips behavior on the next tick without a restart.
func (m *ActivityMonitor) captureScreenshots(ctx context.Context) {
	// time.NewTimer (singleshot) instead of NewTicker — we re-roll
	// the next delay after every fire, so a fixed-period ticker
	// wouldn't give us the per-cycle randomness we want.
	timer := time.NewTimer(nextScreenshotDelay())
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			if m.appState.IsTimerActive() && m.apiClient.ScreenshotsEnabled() {
				m.takeAndUploadScreenshot(ctx)
			}
			// Re-arm with a fresh random delay regardless of
			// whether we actually captured. A continuously-
			// inactive timer should not bunch up its captures
			// the moment activity resumes.
			timer.Reset(nextScreenshotDelay())
		}
	}
}

// takeAndUploadScreenshot captures the screen, uploads to S3, and stores the URL.
// Has panic recovery to prevent screenshot failures from crashing the app.
// Privacy contract still respected via ctx — Stop / Logout cancels
// the goroutine before this runs. The pre-capture warning balloon
// was removed because it was noisy (a tray toast every ~9 min) and
// broadcast capture timing to anyone in shoulder-view.
func (m *ActivityMonitor) takeAndUploadScreenshot(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("Screenshot capture recovered from panic: %v", r)
		}
	}()
	// Honor cancellation that may have arrived just as the timer
	// fired — Stop closing ctx between the timer firing and this
	// function running should still abort cleanly.
	select {
	case <-ctx.Done():
		return
	default:
	}

	// Capture (CaptureScreen checks lock state internally)
	jpegData, err := m.screenshotCap.CaptureScreenDefault()
	if err != nil {
		log.Printf("Screenshot capture failed: %v", err)
		return
	}

	// Generate filename with timestamp
	filename := fmt.Sprintf("screenshot_%s.jpg", time.Now().UTC().Format("20060102_150405"))

	// Upload to S3. Try once directly; on failure, persist to the
	// queue so the drain worker retries in the background.
	cdnURL, err := m.apiClient.UploadScreenshot(jpegData, filename)
	if err != nil {
		log.Printf("Screenshot upload failed (queuing for retry): %v", err)
		// Snapshot the activity bucket NOW (without flushing it to
		// the heartbeat queue) and pin it to the queued JPEG. When
		// the drain worker re-uploads later, it'll inject the
		// recovered S3 URL into THIS bucket and fire it as a
		// heartbeat — so the screenshot stays linked to the exact
		// activity counts that were live when it was captured.
		// Closes the screenshot/heartbeat orphan window the previous
		// "flush bucket with empty URL + retry screenshot alone"
		// path produced.
		m.mu.Lock()
		bucket := m.snapshotBucketLocked("")
		if m.ssQueue != nil {
			if qerr := m.ssQueue.Enqueue(jpegData, filename, bucket); qerr != nil {
				log.Printf("Failed to enqueue screenshot: %v", qerr)
				// Queue write itself failed (disk full?). Fall back
				// to the legacy "no link, but no data lost" path —
				// route the bucket through the heartbeat queue so
				// activity counts at least make it to the backend.
				if m.hbQueue != nil {
					_ = m.hbQueue.Enqueue(bucket)
				}
				return
			}
			log.Printf("Screenshot queued with paired bucket (pending=%d)", m.ssQueue.Count())
			return
		}
		// No screenshot queue available — fall back to flushing the
		// activity bucket without a link so counts still land.
		if m.hbQueue != nil {
			_ = m.hbQueue.Enqueue(bucket)
		}
		return
	}

	log.Printf("Screenshot uploaded: %s (%d KB)", cdnURL, len(jpegData)/1024)

	// Flush the current bucket early with the screenshot attached, so counts
	// reflect real accumulated activity instead of being hardcoded to zero.
	m.sendCurrentBucket(cdnURL)
}

// drainScreenshots replays queued screenshots to S3 via the API
// client's presign+PUT path. Same stop-on-first-failure contract as
// drainHeartbeats.
//
// When a queued entry has a paired heartbeat bucket (written by the
// upload-failure path in takeAndUploadScreenshot), this drain injects
// the recovered S3 URL into that bucket and forwards it to the
// heartbeat queue — so the screenshot gets linked to the activity
// counts captured at its original moment, not to a fresh empty bucket.
// Older entries written without a bucket (or via the no-queue
// fallback) just get re-uploaded; their activity counts already
// went out via the regular heartbeat path.
func (m *ActivityMonitor) drainScreenshots(ctx context.Context) {
	if m.ssQueue == nil {
		return
	}
	sent := m.ssQueue.Drain(ctx, func(jpeg []byte, filename string, bucket map[string]interface{}) error {
		cdnURL, err := m.apiClient.UploadScreenshot(jpeg, filename)
		if err != nil {
			return err
		}
		if bucket == nil || m.hbQueue == nil {
			// Pre-pair-schema entry, or hb queue unavailable —
			// upload succeeded, that's all we can do.
			return nil
		}
		// Inject the recovered URL into the original bucket and
		// route it through the heartbeat queue. We deliberately DO
		// NOT propagate a heartbeat-queue write failure back as the
		// drain error: the screenshot upload already succeeded, so
		// removing it from the screenshot queue is correct. A
		// failed hb-queue write leaves the bucket lost (rare; disk
		// full would have failed the screenshot enqueue too), but
		// is preferable to permanently retrying an already-uploaded
		// screenshot.
		bucket["screenshot_url"] = cdnURL
		if hberr := m.hbQueue.Enqueue(bucket); hberr != nil {
			log.Printf("Screenshot drain: failed to re-link bucket to %s: %v", cdnURL, hberr)
		}
		return nil
	})
	if sent > 0 {
		log.Printf("Screenshot drain: %d uploaded (pending=%d)", sent, m.ssQueue.Count())
		// Kick the heartbeat drain so the just-relinked buckets
		// hit the backend in the same drain pass rather than
		// waiting for the next 30s tick.
		m.drainHeartbeats(ctx)
	}
}

// drainWorker is the background ticker that retries both queues on
// a fixed cadence. Runs independently of new-activity enqueue paths
// so a fully-offline client still catches up when the network
// returns, even if no new buckets are being generated (e.g. user
// idle).
const drainInterval = 30 * time.Second

func (m *ActivityMonitor) drainWorker(ctx context.Context) {
	ticker := time.NewTicker(drainInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.drainHeartbeats(ctx)
			m.drainScreenshots(ctx)
		}
	}
}
