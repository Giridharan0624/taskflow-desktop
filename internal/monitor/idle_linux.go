//go:build linux

package monitor

import (
	"log"
	"os/exec"
	"strconv"
	"strings"
)

// IdleDetector detects how long the user has been idle on Linux.
// Uses xprintidle (X11) which returns idle time in milliseconds.
// Falls back to 0 if xprintidle is not available (e.g., pure Wayland).
type IdleDetector struct{}

func NewIdleDetector() *IdleDetector {
	return &IdleDetector{}
}

// GetIdleSeconds returns seconds since last keyboard/mouse input.
// Uses xprintidle on X11. Returns 0 on Wayland or if xprintidle is missing.
func (d *IdleDetector) GetIdleSeconds() int {
	out, err := exec.Command("xprintidle").Output()
	if err != nil {
		// xprintidle not installed or not on X11 — try xssstate as fallback
		out, err = exec.Command("xssstate", "-i").Output()
		if err != nil {
			return 0
		}
	}

	ms, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		log.Printf("idle: failed to parse output: %v", err)
		return 0
	}

	return ms / 1000
}

func (d *IdleDetector) IsIdle(thresholdSeconds int) bool {
	return d.GetIdleSeconds() > thresholdSeconds
}
