//go:build linux

package monitor

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// WindowTracker tracks the active foreground window on Linux.
// Uses xdotool to get the active window PID, then reads /proc/[pid]/exe.
type WindowTracker struct{}

func NewWindowTracker() *WindowTracker {
	return &WindowTracker{}
}

// GetActiveWindowApp returns the friendly name of the foreground application.
// Uses xdotool on X11. Returns "" on pure Wayland or if xdotool is missing.
func (w *WindowTracker) GetActiveWindowApp() string {
	// Get active window ID
	winID, err := exec.Command("xdotool", "getactivewindow").Output()
	if err != nil {
		return ""
	}

	// Get PID of the window
	pidOut, err := exec.Command("xdotool", "getwindowpid", strings.TrimSpace(string(winID))).Output()
	if err != nil {
		return ""
	}

	pid := strings.TrimSpace(string(pidOut))
	if pid == "" || pid == "0" {
		return ""
	}

	// Read the executable path from /proc/[pid]/exe
	exePath, err := os.Readlink(fmt.Sprintf("/proc/%s/exe", pid))
	if err != nil {
		return "Other"
	}

	return friendlyAppName(exePath)
}
