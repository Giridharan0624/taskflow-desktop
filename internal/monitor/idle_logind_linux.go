//go:build linux

package monitor

import (
	"os"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
)

// logind (systemd-logind) exposes an IdleHint / IdleSinceHint pair per
// user session on the system D-Bus. The desktop environment (GNOME
// Mutter, KDE KScreenLocker, Sway-idle, etc.) sets these via
// SetIdleHint() on whatever cadence it chooses — GNOME updates roughly
// every 30s, but the values are always CORRECT regardless of display
// server. That's why we need this backend: MIT-SCREEN-SAVER via
// XWayland only sees X11 apps, so a user typing exclusively in native
// Wayland apps appears "idle forever" and the keyboard heuristic
// counts every tick as a false press.
//
// Tradeoff vs MIT-SCREEN-SAVER:
//   - Coarser granularity (30s vs sub-second) — for our <2s "was user
//     active?" heuristic this means a brief window of over-counting
//     right after the user stops typing. Acceptable.
//   - Requires a systemd user session, which every mainstream Linux
//     distro ships (Ubuntu, Debian, Fedora, Arch default, openSUSE,
//     Manjaro). Non-systemd distros — Void, Artix, Gentoo with OpenRC —
//     fall through to the MIT-SCREEN-SAVER backend.

var (
	logindOnce    sync.Once
	logindSession dbus.BusObject
)

// Note on shutdown: dbus.SystemBus() returns a PROCESS-shared connection
// that godbus itself manages. Explicitly closing it from here would
// affect any other D-Bus consumer (none today, but cross-package
// sharing breaks if we assume otherwise) and the OS reclaims the
// socket on process exit anyway. No Close() helper is exposed.

// getLogindSession returns the per-session D-Bus proxy (nil if logind
// is unavailable or the session can't be resolved). Caller MUST nil-
// check.
func getLogindSession() dbus.BusObject {
	logindOnce.Do(func() {
		conn, err := dbus.SystemBus()
		if err != nil {
			return
		}
		mgr := conn.Object(
			"org.freedesktop.login1",
			dbus.ObjectPath("/org/freedesktop/login1"),
		)

		var sessionPath dbus.ObjectPath
		if sid := os.Getenv("XDG_SESSION_ID"); sid != "" {
			if err := mgr.Call("org.freedesktop.login1.Manager.GetSession", 0, sid).Store(&sessionPath); err != nil {
				return
			}
		} else {
			if err := mgr.Call("org.freedesktop.login1.Manager.GetSessionByPID", 0, uint32(os.Getpid())).Store(&sessionPath); err != nil {
				return
			}
		}
		if sessionPath == "" {
			return
		}
		logindSession = conn.Object("org.freedesktop.login1", sessionPath)
	})
	return logindSession
}

// logindIdleSeconds reports idle seconds via login1.Session.IdleHint /
// IdleSinceHint. Returns (seconds, true) when logind answered. Returns
// (0, false) when logind is unreachable — caller should try another
// backend.
func logindIdleSeconds() (int, bool) {
	sess := getLogindSession()
	if sess == nil {
		return 0, false
	}

	// IdleHint: bool. false => user is active right now.
	var hint dbus.Variant
	if err := sess.Call(
		"org.freedesktop.DBus.Properties.Get", 0,
		"org.freedesktop.login1.Session", "IdleHint",
	).Store(&hint); err != nil {
		return 0, false
	}
	idle, ok := hint.Value().(bool)
	if !ok {
		return 0, false
	}
	if !idle {
		return 0, true
	}

	// IdleSinceHint: uint64, microseconds-since-epoch (CLOCK_REALTIME)
	// when the DE last flipped IdleHint to true. 0 means "never".
	var since dbus.Variant
	if err := sess.Call(
		"org.freedesktop.DBus.Properties.Get", 0,
		"org.freedesktop.login1.Session", "IdleSinceHint",
	).Store(&since); err != nil {
		return 0, false
	}
	sinceMicros, ok := since.Value().(uint64)
	if !ok || sinceMicros == 0 {
		return 0, true
	}
	elapsed := time.Now().UnixMicro() - int64(sinceMicros)
	if elapsed < 0 {
		// Clock skew or NTP step — treat as "just became idle".
		return 0, true
	}
	return int(elapsed / 1_000_000), true
}
