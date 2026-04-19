package monitor

import (
	"bytes"
	"fmt"
	"image"
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

	// Scale down to 50% for smaller file size
	scaledW := w / 2
	scaledH := h / 2
	scaled := image.NewRGBA(image.Rect(0, 0, scaledW, scaledH))
	for y := 0; y < scaledH; y++ {
		for x := 0; x < scaledW; x++ {
			srcX := x * 2
			srcY := y * 2
			srcIdx := (srcY*img.Stride) + srcX*4
			dstIdx := (y*scaled.Stride) + x*4
			if srcIdx+3 < len(img.Pix) && dstIdx+3 < len(scaled.Pix) {
				scaled.Pix[dstIdx+0] = img.Pix[srcIdx+0]
				scaled.Pix[dstIdx+1] = img.Pix[srcIdx+1]
				scaled.Pix[dstIdx+2] = img.Pix[srcIdx+2]
				scaled.Pix[dstIdx+3] = 255
			}
		}
	}

	// Encode to JPEG
	var buf bytes.Buffer
	err = jpeg.Encode(&buf, scaled, &jpeg.Options{Quality: quality})
	if err != nil {
		return nil, fmt.Errorf("JPEG encode failed: %w", err)
	}

	log.Printf("Screenshot captured: %dx%d → %dx%d, %d KB JPEG",
		w, h, scaledW, scaledH, buf.Len()/1024)

	return buf.Bytes(), nil
}

// CaptureScreenDefault takes a screenshot with default quality (60).
func (sc *ScreenshotCapture) CaptureScreenDefault() ([]byte, error) {
	return sc.CaptureScreen(60)
}

// ShowNotification logs a notification (actual display is via tray balloon in activity.go).
func ShowNotification(title, message string) {
	log.Printf("Notification: %s - %s", title, message)
}
