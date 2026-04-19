package monitor

import (
	"bytes"
	"fmt"
	"image/jpeg"
	"log"

	"github.com/kbinani/screenshot"
)

// ScreenshotCapture handles taking screenshots using DXGI Desktop Duplication (Windows)
// or platform-native APIs (Linux: X11, macOS: CoreGraphics).
// This replaces the old Win32 BitBlt approach which caused GPU conflicts with video
// conferencing apps (Google Meet, Zoom, Teams) on RTX GPUs.
//
// idle is cached at construction so IsScreenLocked doesn't allocate a
// fresh detector per screenshot — pattern-consistency with
// InputTracker. See V2-M3.
type ScreenshotCapture struct {
	idle *IdleDetector
}

// NewScreenshotCapture creates a new screenshot capture instance.
func NewScreenshotCapture() *ScreenshotCapture {
	return &ScreenshotCapture{
		idle: NewIdleDetector(),
	}
}

// IsScreenLocked reports whether the user's desktop session is
// currently locked.
//
// Resolution order:
//  1. Native OS API per platform (nativeIsScreenLocked below):
//     - Windows: WTSQuerySessionInformation / WTS_SESSIONSTATE_LOCK
//     - Linux:   org.freedesktop.login1.Session.LockedHint
//     - macOS:   not implemented yet, falls through to idle proxy
//  2. Fallback: idle > 10 min heuristic (the pre-Phase-3 behavior)
//
// The native path closes the two-sided failure mode of the idle
// proxy: a mouse-jiggler no longer keeps the capture running on a
// locked screen, and a user reading a long doc for >10 min is no
// longer skipped. When the native call returns ok=false (API
// unavailable, D-Bus down, etc.) we silently fall through to the
// idle heuristic — the degradation is the same as the pre-fix
// behavior, never worse. See H-MON-3.
func (sc *ScreenshotCapture) IsScreenLocked() bool {
	if locked, ok := nativeIsScreenLocked(); ok {
		return locked
	}
	if sc.idle == nil {
		// Defensive: constructor always sets idle; only paths that
		// skip NewScreenshotCapture would land here.
		return false
	}
	return sc.idle.GetIdleSeconds() > 600
}

// CaptureScreen takes a screenshot and returns it as JPEG bytes.
// Uses kbinani/screenshot which uses:
//   - Windows: DXGI Desktop Duplication (GPU-friendly, no BitBlt)
//   - Linux: X11 XGetImage
//   - macOS: CoreGraphics CGWindowListCreateImage
//
// Previously this nearest-neighbour-downscaled to 50% and JPEG-
// compressed at quality 60 — intended as a file-size optimisation
// but it made on-screen text unreadable in the activity-report
// viewer (a reviewer checking whether the user was on the right
// task could not tell one IDE window from another). The
// 2 MB uploads it now produces at native 1920×1080 JPEG-q85 sit
// well inside the S3 upload client's 3-minute timeout (M-API-2
// already bumped that) and a typical 8-hour day at one screenshot
// per 10 min is ~48 × 2 MB = ~100 MB — acceptable for the use case
// (activity monitoring, not frame-by-frame video).
func (sc *ScreenshotCapture) CaptureScreen(quality int) ([]byte, error) {
	if sc.IsScreenLocked() {
		return nil, fmt.Errorf("screen is locked")
	}

	// Get number of displays
	n := screenshot.NumActiveDisplays()
	if n == 0 {
		return nil, fmt.Errorf("no active displays found")
	}

	// Capture primary display (index 0)
	bounds := screenshot.GetDisplayBounds(0)
	img, err := screenshot.CaptureRect(bounds)
	if err != nil {
		return nil, fmt.Errorf("screen capture failed: %w", err)
	}

	w := bounds.Dx()
	h := bounds.Dy()

	// Encode to JPEG at native resolution.
	var buf bytes.Buffer
	err = jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality})
	if err != nil {
		return nil, fmt.Errorf("JPEG encode failed: %w", err)
	}

	log.Printf("Screenshot captured: %dx%d native, %d KB JPEG", w, h, buf.Len()/1024)

	return buf.Bytes(), nil
}

// CaptureScreenDefault takes a screenshot at the default quality.
// 85 is the sweet spot for JPEG — subjectively indistinguishable from
// q100 for photographic content, ~2–3× smaller file than q95 for
// screenshots that contain text. Raised from 60 because the old
// setting visibly softened on-screen UI text (the reviewer's main
// signal when auditing activity reports).
func (sc *ScreenshotCapture) CaptureScreenDefault() ([]byte, error) {
	return sc.CaptureScreen(85)
}

// ShowNotification logs a notification (actual display is via tray balloon in activity.go).
func ShowNotification(title, message string) {
	log.Printf("Notification: %s - %s", title, message)
}
