//go:build linux

package monitor

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"log"
	"os"
	"os/exec"
	"strings"
)

// ScreenshotCapture handles taking screenshots on Linux.
// Tries gnome-screenshot, scrot, then import (ImageMagick) in order.
type ScreenshotCapture struct{}

func NewScreenshotCapture() *ScreenshotCapture {
	return &ScreenshotCapture{}
}

// IsScreenLocked checks if the session is locked via loginctl.
func (sc *ScreenshotCapture) IsScreenLocked() bool {
	// Get the current session ID
	out, err := exec.Command("loginctl", "show-session", "auto", "-p", "LockedHint", "--value").Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == "yes"
}

// CaptureScreen takes a screenshot and returns JPEG bytes.
func (sc *ScreenshotCapture) CaptureScreen(quality int) ([]byte, error) {
	if sc.IsScreenLocked() {
		return nil, fmt.Errorf("screen is locked")
	}

	tmpFile := "/tmp/taskflow-screenshot.png"
	defer os.Remove(tmpFile)

	// Try screenshot tools in order of preference
	captured := false
	tools := []struct {
		name string
		args []string
	}{
		{"gnome-screenshot", []string{"-f", tmpFile}},
		{"scrot", []string{tmpFile}},
		{"import", []string{"-window", "root", tmpFile}}, // ImageMagick
		{"grim", []string{tmpFile}},                       // Wayland (sway, etc.)
	}

	for _, tool := range tools {
		if _, err := exec.LookPath(tool.name); err != nil {
			continue
		}
		if err := exec.Command(tool.name, tool.args...).Run(); err != nil {
			continue
		}
		captured = true
		break
	}

	if !captured {
		return nil, fmt.Errorf("no screenshot tool found (install gnome-screenshot, scrot, or grim)")
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

	// Scale down to 50%
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
	// Use notify-send (available on most Linux desktops)
	exec.Command("notify-send", title, message, "-t", "5000").Start()
}
