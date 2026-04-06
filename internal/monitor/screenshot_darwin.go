//go:build darwin

package monitor

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png"
	"log"
	"os"
	"os/exec"
	"strings"
)

// ScreenshotCapture handles taking screenshots on macOS.
// Uses the built-in screencapture CLI tool (no dependencies needed).
// Requires Screen Recording permission (System Preferences > Privacy > Screen Recording).
type ScreenshotCapture struct{}

func NewScreenshotCapture() *ScreenshotCapture {
	return &ScreenshotCapture{}
}

// IsScreenLocked checks if the macOS screen is locked.
func (sc *ScreenshotCapture) IsScreenLocked() bool {
	// Check if the screen saver or lock screen is active
	out, err := exec.Command("python3", "-c",
		`import Quartz; d=Quartz.CGSessionCopyCurrentDictionary(); print(d.get("CGSSessionScreenIsLocked", 0))`,
	).Output()
	if err != nil {
		// Fallback: check idle time
		idle := NewIdleDetector()
		return idle.GetIdleSeconds() > 600
	}
	return strings.TrimSpace(string(out)) == "1"
}

// CaptureScreen takes a screenshot and returns JPEG bytes.
func (sc *ScreenshotCapture) CaptureScreen(quality int) ([]byte, error) {
	if sc.IsScreenLocked() {
		return nil, fmt.Errorf("screen is locked")
	}

	tmpFile := os.TempDir() + "/taskflow-screenshot.png"
	defer os.Remove(tmpFile)

	// -x suppresses the shutter sound
	if err := exec.Command("screencapture", "-x", "-t", "png", tmpFile).Run(); err != nil {
		return nil, fmt.Errorf("screencapture failed: %w", err)
	}

	// Read the captured file
	data, err := os.ReadFile(tmpFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read screenshot: %w", err)
	}

	// Decode, scale to 50%, re-encode as JPEG
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("failed to decode screenshot: %w", err)
	}

	bounds := img.Bounds()
	w := bounds.Dx() / 2
	h := bounds.Dy() / 2
	scaled := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			scaled.Set(x, y, img.At(x*2, y*2))
		}
	}

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, scaled, &jpeg.Options{Quality: quality}); err != nil {
		return nil, fmt.Errorf("JPEG encode failed: %w", err)
	}

	log.Printf("Screenshot captured: %dx%d → %dx%d, %d KB JPEG",
		bounds.Dx(), bounds.Dy(), w, h, buf.Len()/1024)

	return buf.Bytes(), nil
}

func (sc *ScreenshotCapture) CaptureScreenDefault() ([]byte, error) {
	return sc.CaptureScreen(60)
}

func ShowNotification(title, message string) {
	log.Printf("Notification: %s — %s", title, message)
	exec.Command("osascript", "-e",
		fmt.Sprintf(`display notification "%s" with title "%s"`, message, title),
	).Start()
}
