//go:build linux

package main

import "os"

// detectSessionInfo reports Linux display-server capabilities.
//
// On Wayland the compositor (by design) does not let non-privileged
// apps see which window has focus. This isn't a limitation we can
// engineer around — it's a Wayland security property. We surface it
// honestly so users know per-app breakdown will be partial.
//
// XWayland-hosted apps (Chrome, VS Code, Discord, Steam — many of the
// most-used desktop apps on Linux still run under XWayland on Ubuntu
// 24.04) are still trackable because they live in an X11 world our
// jezek/xgb code can query. Only native Wayland apps fall through to
// the "Desktop" bucket.
func detectSessionInfo() *SessionInfo {
	info := &SessionInfo{Platform: "linux"}

	switch os.Getenv("XDG_SESSION_TYPE") {
	case "wayland":
		info.SessionType = "wayland"
		info.CanTrackWindows = false
		info.LimitationMessage = "You're on a Wayland session. Native Wayland apps will show as \"Desktop\" in activity reports — per-app breakdown only works for apps running under XWayland. For full tracking, sign out and choose \"Ubuntu on Xorg\" at the login screen."
	case "x11", "":
		// Empty XDG_SESSION_TYPE happens on older distros and on
		// systems where the user logs in outside a systemd session —
		// in practice this means X11.
		info.SessionType = "x11"
		info.CanTrackWindows = true
	default:
		// "tty", "mir", or anything else future compositors invent.
		info.SessionType = "unknown"
		info.CanTrackWindows = false
		info.LimitationMessage = "Unrecognized session type — activity tracking may be incomplete."
	}
	return info
}
