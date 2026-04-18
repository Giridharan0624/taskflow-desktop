//go:build darwin

package main

// macOS exposes the frontmost app through NSWorkspace without
// Accessibility permission, and the app's bundle identifier is enough
// for the tracker's friendly-name lookup. No session-type caveat.
func detectSessionInfo() *SessionInfo {
	return &SessionInfo{
		Platform:        "darwin",
		SessionType:     "native",
		CanTrackWindows: true,
	}
}
