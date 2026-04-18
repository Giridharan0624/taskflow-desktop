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
type ScreenshotCapture struct{}

// NewScreenshotCapture creates a new screenshot capture instance.
func NewScreenshotCapture() *ScreenshotCapture {
	return &ScreenshotCapture{}
}

// IsScreenLocked checks if the user's session is likely locked.
// Uses idle time as a heuristic — if idle for over 10 minutes, likely locked.
//
// TODO(H-MON-3): this is a two-sided proxy with known failure modes:
//   - False negative (privacy breach): a locked screen with a mouse
//     jiggler or auto-moving cursor reads as active and gets captured.
//   - False positive (lost data): a user reading a long document for
//     >10 minutes reads as locked and the capture is skipped.
//
// Replacing this with native APIs requires platform-specific code:
//   - Windows: WTSQuerySessionInformation(WTSSessionInfoEx) + check
//     WTSINFOEX_LEVEL1.SessionFlags for WTS_SESSIONSTATE_LOCK.
//   - macOS:   CGSessionCopyCurrentDictionary + CGSSessionScreenIsLocked
//     via cgo.
//   - Linux:   loginctl / org.freedesktop.login1 D-Bus, LockedHint
//     property on the current session.
//
// Left as-is for now because a half-implemented native check that gets
// the failure modes wrong is worse than the current proxy.
func (sc *ScreenshotCapture) IsScreenLocked() bool {
	idle := NewIdleDetector()
	return idle.GetIdleSeconds() > 600
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
