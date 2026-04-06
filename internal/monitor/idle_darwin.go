//go:build darwin

package monitor

import (
	"log"
	"os/exec"
	"strconv"
	"strings"
)

// IdleDetector detects how long the user has been idle on macOS.
// Uses ioreg to read the HIDIdleTime property from IOKit (no CGo needed).
type IdleDetector struct{}

func NewIdleDetector() *IdleDetector {
	return &IdleDetector{}
}

// GetIdleSeconds returns seconds since last keyboard/mouse input.
// Parses HIDIdleTime from ioreg (reported in nanoseconds).
func (d *IdleDetector) GetIdleSeconds() int {
	out, err := exec.Command("ioreg", "-c", "IOHIDSystem", "-d", "4").Output()
	if err != nil {
		log.Printf("idle: ioreg failed: %v", err)
		return 0
	}

	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, "HIDIdleTime") && strings.Contains(line, "=") {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) != 2 {
				continue
			}
			valStr := strings.TrimSpace(parts[1])
			ns, err := strconv.ParseInt(valStr, 10, 64)
			if err != nil {
				continue
			}
			return int(ns / 1_000_000_000) // nanoseconds to seconds
		}
	}

	return 0
}

func (d *IdleDetector) IsIdle(thresholdSeconds int) bool {
	return d.GetIdleSeconds() > thresholdSeconds
}
