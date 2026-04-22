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

// logindRetryCooldown bounds how often we re-attempt the D-Bus proxy
// setup after a failure. Without this, every logindIdleSeconds call
// during a logind outage would re-run GetSessionByPID / GetSession —
// amplifying the outage into a hot loop of D-Bus traffic. A one-
// minute cooldown gives logind time to come back after a restart
// without pathological retry behaviour. See V2-M4.
const logindRetryCooldown = 60 * time.Second

var (
	logindMu         sync.Mutex
	logindSession    dbus.BusObject
	logindInitDone   bool
	logindLastTryAt  time.Time
)

// Note on shutdown: dbus.SystemBus() returns a PROCESS-shared connection
// that godbus itself manages. Explicitly closing it from here would
// affect any other D-Bus consumer (none today, but cross-package
// sharing breaks if we assume otherwise) and the OS reclaims the
// socket on process exit anyway. No Close() helper is exposed.

// getLogindSession returns the per-session D-Bus proxy (nil if logind
// is unavailable or the session can't be resolved). Caller MUST nil-
// check. If a previous init failed, subsequent calls retry at most
// once per logindRetryCooldown — this supports recovery from a logind
// restart (rare but observed during distro upgrades) without hot-
// looping during a sustained outage.
func getLogindSession() dbus.BusObject {
	logindMu.Lock()
	defer logindMu.Unlock()
	if logindSession != nil {
		return logindSession
	}
	if logindInitDone && time.Since(logindLastTryAt) < logindRetryCooldown {
		return nil
	}
	logindInitDone = true
	logindLastTryAt = time.Now()

	conn, err := dbus.SystemBus()
	if err != nil {
		return nil
	}
	mgr := conn.Object(
		"org.freedesktop.login1",
		dbus.ObjectPath("/org/freedesktop/login1"),
	)

	var sessionPath dbus.ObjectPath
	if sid := os.Getenv("XDG_SESSION_ID"); sid != "" {
		if err := mgr.Call("org.freedesktop.login1.Manager.GetSession", 0, sid).Store(&sessionPath); err != nil {
			return nil
		}
	} else {
		if err := mgr.Call("org.freedesktop.login1.Manager.GetSessionByPID", 0, uint32(os.Getpid())).Store(&sessionPath); err != nil {
			return nil
		}
	}
	if sessionPath == "" {
		return nil
	}
	logindSession = conn.Object("org.freedesktop.login1", sessionPath)
	return logindSession
}

// invalidateLogindSession clears the cached proxy so the next
// getLogindSession call rebuilds it from scratch. Called from
// logindIdleSeconds on D-Bus errors that look like the session object
// has gone stale (logind crashed / restarted mid-run).
func invalidateLogindSession() {
	logindMu.Lock()
	logindSession = nil
	// Leave logindInitDone=true + logindLastTryAt as-is so the
	// cooldown keeps us from hot-retrying.
	logindMu.Unlock()
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
		// Most likely cause is a logind restart — the cached proxy
		// now points at a dead connection. Drop the cache so the
		// next call retries (subject to the cooldown).
		invalidateLogindSession()
		return 0, false
	}

	// Refresh the retry timestamp on every successful answer. Without
	// this, after a logind restart the cooldown-gated retry path
	// (getLogindSession) would keep returning nil for up to 60 s
	// because logindLastTryAt is frozen from initial success. The
	// keyboard idle heuristic then reads 0 idle → counts every tick
	// as an active keystroke, inflating the keyboard counter for the
	// duration of the outage. See V3-H9.
	logindMu.Lock()
	logindLastTryAt = time.Now()
	logindMu.Unlock()
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
		invalidateLogindSession()
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
	// Cap at one day to defuse a stale IdleSinceHint from before a
	// long suspend/resume or clock jump. The <2s "active?" heuristic
	// downstream doesn't care about absolute values beyond a few
	// minutes, so a cap keeps the returned number useful.
	const maxIdle = int64(24 * 3600 * 1_000_000)
	if elapsed > maxIdle {
		elapsed = maxIdle
	}
	return int(elapsed / 1_000_000), true
}
