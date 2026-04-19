//go:build linux

package monitor

import "github.com/godbus/dbus/v5"

// nativeIsScreenLocked reads org.freedesktop.login1.Session.LockedHint
// via D-Bus. GNOME Mutter, KDE KScreenLocker, and swayidle all set this
// when the screen locker activates — no heuristic, no false-positives
// from a user reading a long document.
//
// Returns (_, false) when the session proxy is unreachable (non-systemd
// distro, D-Bus down, logind restart race), so caller falls back to
// the idle proxy.
func nativeIsScreenLocked() (bool, bool) {
	sess := getLogindSession()
	if sess == nil {
		return false, false
	}
	var hint dbus.Variant
	err := sess.Call(
		"org.freedesktop.DBus.Properties.Get", 0,
		"org.freedesktop.login1.Session", "LockedHint",
	).Store(&hint)
	if err != nil {
		return false, false
	}
	locked, ok := hint.Value().(bool)
	if !ok {
		return false, false
	}
	return locked, true
}
