//go:build darwin

package monitor

import (
	"os/exec"
	"strings"
)

// WindowTracker tracks the active foreground window on macOS.
// Uses osascript (AppleScript) to get the frontmost application name.
type WindowTracker struct{}

func NewWindowTracker() *WindowTracker {
	return &WindowTracker{}
}

// GetActiveWindowApp returns the friendly name of the foreground application.
func (w *WindowTracker) GetActiveWindowApp() string {
	out, err := exec.Command("osascript", "-e",
		`tell application "System Events" to get name of first application process whose frontmost is true`,
	).Output()
	if err != nil {
		return ""
	}

	name := strings.TrimSpace(string(out))
	if name == "" {
		return ""
	}

	return friendlyAppName(name)
}
