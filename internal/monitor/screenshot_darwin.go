//go:build darwin

package monitor

// nativeIsScreenLocked on macOS would use
// CGSessionCopyCurrentDictionary and check the
// "CGSSessionScreenIsLocked" key. That requires additional Objective-C
// bridging beyond the current scope — the property isn't a public API
// and the helpers aren't exposed by cgo headers without extra work.
//
// For now we return (_, false) so the caller falls back to the idle
// heuristic, preserving pre-Phase-3 behavior on macOS. See H-MON-3.
func nativeIsScreenLocked() (bool, bool) {
	return false, false
}
