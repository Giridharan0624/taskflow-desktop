//go:build linux

package monitor

import (
	"fmt"
	"os"
)

// WindowTracker reports the active foreground application on Linux.
//
// Under X11 we resolve the focus via EWMH atoms on the shared X
// connection in x11_linux.go — _NET_ACTIVE_WINDOW on the root window
// plus _NET_WM_PID on the focused window → /proc/[pid]/exe →
// friendlyAppName. Same data the old `xdotool` shell-out produced,
// no subprocess.
//
// Under Wayland we INTENTIONALLY do not try to read X atoms, even
// though XWayland provides them. The reason: GNOME Mutter updates
// XWayland's _NET_ACTIVE_WINDOW when an XWayland client gains focus,
// but it does NOT reset the atom when focus moves to a native
// Wayland client. The result is stale data — a user who opens Chrome
// (XWayland), then switches to Files (native Wayland), would still
// see "Chrome" as the active app for the entire Files session.
//
// Silently-wrong activity reports are worse than honestly-incomplete
// ones, so we return a stable "Desktop" bucket on Wayland and rely
// on the SessionBanner (see session_linux.go / TimerView) to tell
// the user why per-app breakdown is limited. A user who needs full
// fidelity can switch to "Ubuntu on Xorg" at the login screen.
type WindowTracker struct {
	isWayland bool
}

func NewWindowTracker() *WindowTracker {
	return &WindowTracker{
		isWayland: os.Getenv("XDG_SESSION_TYPE") == "wayland",
	}
}

// GetActiveWindowApp returns the friendly name of the foreground
// application. Returns "" if no X display is reachable, "Desktop"
// under Wayland, or the resolved app name under X11.
func (w *WindowTracker) GetActiveWindowApp() string {
	if w.isWayland {
		// Wayland compositors don't expose focus to non-privileged
		// apps by design. Return a stable bucket so the activity
		// report has a consistent category instead of many empty or
		// stale XWayland entries.
		return "Desktop"
	}

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

	// Windows without _NET_WM_PID (some JVM apps, a few remote-X
	// clients) still have a title — return that so the user sees a
	// meaningful label instead of "Other". Legacy apps that set only
	// the ancient WM_NAME get picked up by the fallback inside
	// getWindowTitle.
	if title := x.getWindowTitle(win); title != "" {
		return title
	}
	return "Other"
}
