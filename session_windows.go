//go:build windows

package main

// Windows has no Wayland-style isolation — every visible window can be
// queried by any process via GetForegroundWindow / GetWindowThreadProcessId.
// Per-app breakdown is always available.
func detectSessionInfo() *SessionInfo {
	return &SessionInfo{
		Platform:        "windows",
		SessionType:     "native",
		CanTrackWindows: true,
	}
}
