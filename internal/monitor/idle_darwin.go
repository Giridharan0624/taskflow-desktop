//go:build darwin

package monitor

import (
	"context"
	"log"
	"os/exec"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// IdleDetector detects how long the user has been idle on macOS.
// Uses ioreg to read the HIDIdleTime property from IOKit (no CGo needed).
//
// ioreg is called with a 500 ms timeout via exec.CommandContext. On
// timeout or failure the previous cached value is returned; without
// the cap, a hung ioreg would stall the 1-second activity tick (no
// heartbeat, no screenshot cadence). See V3-H2.
type IdleDetector struct {
	lastIdleSec atomic.Int64
}

func NewIdleDetector() *IdleDetector {
	return &IdleDetector{}
}

const ioregTimeout = 500 * time.Millisecond

// GetIdleSeconds returns seconds since last keyboard/mouse input.
// Parses HIDIdleTime from ioreg (reported in nanoseconds).
func (d *IdleDetector) GetIdleSeconds() int {
	ctx, cancel := context.WithTimeout(context.Background(), ioregTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ioreg", "-c", "IOHIDSystem", "-d", "4").Output()
	if err != nil {
		if ctx.Err() != nil {
			log.Printf("idle: ioreg timed out after %s — returning cached value", ioregTimeout)
		} else {
			log.Printf("idle: ioreg failed: %v", err)
		}
		return int(d.lastIdleSec.Load())
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
			sec := int64(ns / 1_000_000_000) // nanoseconds to seconds
			d.lastIdleSec.Store(sec)
			return int(sec)
		}
	}

	return int(d.lastIdleSec.Load())
}

func (d *IdleDetector) IsIdle(thresholdSeconds int) bool {
	return d.GetIdleSeconds() > thresholdSeconds
}
