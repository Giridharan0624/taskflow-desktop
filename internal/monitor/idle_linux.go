//go:build linux

package monitor

import "os"

// IdleDetector reports how long the user has been idle on Linux.
//
// Backend selection (best-effort, in priority order):
//
//   1. Pure X11 session (XDG_SESSION_TYPE=x11 or empty): MIT-SCREEN-
//      SAVER via xgb. Sub-second precision, no dependencies beyond an
//      X server.
//   2. Wayland session (XDG_SESSION_TYPE=wayland): systemd-logind's
//      IdleHint / IdleSinceHint on the system D-Bus. Coarser (~30s
//      granularity, set by the DE) but CORRECT — MIT-SCREEN-SAVER via
//      XWayland only sees X11 clients, so a user typing in native
//      Wayland apps would otherwise look idle forever and the keyboard
//      heuristic would falsely count every poll tick.
//   3. Fallback: the other backend, then 0.
//
// Returning 0 means "no backend available" — same contract every
// caller has relied on since the original xprintidle-shell-out
// version: activity tracking silently degrades, the app keeps
// working. See Phase 2 plan in CROSS-PLATFORM-PLAN.md.
type IdleDetector struct{}

func NewIdleDetector() *IdleDetector {
	return &IdleDetector{}
}

// GetIdleSeconds returns seconds since last keyboard/mouse input.
func (d *IdleDetector) GetIdleSeconds() int {
	switch os.Getenv("XDG_SESSION_TYPE") {
	case "wayland":
		if sec, ok := logindIdleSeconds(); ok {
			return sec
		}
		// Fall through to X11 — XWayland may at least see X11 focus.
		return getX11().getIdleMs() / 1000
	default:
		// Treat unset / x11 / tty as X11-preferred. MIT-SCREEN-SAVER is
		// authoritative for X11 sessions; if we can't reach an X
		// server at all the helper returns 0 and we try logind.
		if ms := getX11().getIdleMs(); ms > 0 {
			return ms / 1000
		}
		if sec, ok := logindIdleSeconds(); ok {
			return sec
		}
		return 0
	}
}

func (d *IdleDetector) IsIdle(thresholdSeconds int) bool {
	return d.GetIdleSeconds() > thresholdSeconds
}
