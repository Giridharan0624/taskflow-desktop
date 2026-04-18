//go:build linux

package monitor

// IdleDetector reports how long the user has been idle on Linux.
//
// Previously this shelled out to `xprintidle` (a 30-line wrapper around
// the MIT-SCREEN-SAVER extension) and to `xssstate` as a fallback. Both
// were external binaries the user had to install separately and whose
// package names differed per distro (libxprintidle-bin on Debian vs
// xprintidle on Fedora vs AUR on Arch). We now hit MIT-SCREEN-SAVER
// directly via x11_linux.go's cached connection — no subprocess, no
// per-distro package hunt, same data.
//
// On pure-Wayland sessions without XWayland, or X servers built without
// MIT-SCREEN-SAVER, GetIdleSeconds returns 0 — same contract as the
// previous "xprintidle not installed" branch.
type IdleDetector struct{}

func NewIdleDetector() *IdleDetector {
	return &IdleDetector{}
}

// GetIdleSeconds returns seconds since last keyboard/mouse input.
func (d *IdleDetector) GetIdleSeconds() int {
	return getX11().getIdleMs() / 1000
}

func (d *IdleDetector) IsIdle(thresholdSeconds int) bool {
	return d.GetIdleSeconds() > thresholdSeconds
}
