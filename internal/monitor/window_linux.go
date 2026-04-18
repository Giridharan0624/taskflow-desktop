//go:build linux

package monitor

import (
	"fmt"
	"os"
)

// WindowTracker reports the active foreground application on Linux.
//
// Previously this shelled out to `xdotool getactivewindow` +
// `xdotool getwindowpid`. We now resolve those via EWMH atoms on the
// shared X connection in x11_linux.go — same data, no subprocess spawn.
//
// Resolution order:
//  1. _NET_ACTIVE_WINDOW on the root window → active window ID
//  2. _NET_WM_PID on that window → process ID → /proc/[pid]/exe → friendlyAppName
//  3. Fallback: _NET_WM_NAME (or legacy WM_NAME) as the app label
//  4. Final fallback: "Other"
type WindowTracker struct{}

func NewWindowTracker() *WindowTracker {
	return &WindowTracker{}
}

// GetActiveWindowApp returns the friendly name of the foreground
// application, or "" if no X display is reachable.
func (w *WindowTracker) GetActiveWindowApp() string {
	x := getX11()
	if x == nil {
		return ""
	}
	win := x.getActiveWindow()
	if win == 0 {
		return ""
	}

	// Prefer the PID → /proc/[pid]/exe path. friendlyAppName strips
	// the path to a readable name (e.g. /usr/bin/firefox → "Firefox").
	if pid := x.getWindowPID(win); pid > 0 {
		if exe, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid)); err == nil {
			return friendlyAppName(exe)
		}
	}

	// Windows without _NET_WM_PID (e.g. some JVM apps, a few remote-X
	// clients) still have a title — return that so the user sees a
	// meaningful label instead of "Other". Legacy apps that set only
	// the ancient WM_NAME get picked up by the fallback inside
	// getWindowTitle.
	if title := x.getWindowTitle(win); title != "" {
		return title
	}
	return "Other"
}
