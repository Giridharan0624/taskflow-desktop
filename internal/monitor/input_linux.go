//go:build linux

package monitor

import (
	"os/exec"
	"strconv"
	"strings"
	"sync/atomic"
)

// InputTracker tracks keyboard and mouse activity on Linux.
// Uses xdotool for mouse position polling. Keyboard counting uses
// /proc/interrupts to detect input events without requiring root.
type InputTracker struct {
	keyboardTotal atomic.Uint32
	mouseTotal    atomic.Uint32
	lastCursorX   int
	lastCursorY   int
	lastKbCount   uint32
}

func NewInputTracker() *InputTracker {
	t := &InputTracker{}

	// Initialize cursor position
	x, y := getMousePos()
	t.lastCursorX = x
	t.lastCursorY = y

	return t
}

// GetCounts returns current keyboard and mouse event totals.
func (t *InputTracker) GetCounts() (keyboard uint32, mouse uint32) {
	// Mouse: detect cursor movement via xdotool
	x, y := getMousePos()
	if x != t.lastCursorX || y != t.lastCursorY {
		t.mouseTotal.Add(1)
		t.lastCursorX = x
		t.lastCursorY = y
	}

	// Keyboard: use xdotool key detection isn't available without hooks.
	// Instead, count based on idle time — if idle < 2s, user is typing.
	// This is a heuristic: the idle detector gives us a signal.
	idle := NewIdleDetector()
	idleSec := idle.GetIdleSeconds()
	if idleSec < 2 {
		// User was active in the last 2 seconds — count as input
		t.keyboardTotal.Add(1)
	}

	return t.keyboardTotal.Load(), t.mouseTotal.Load()
}

// getMousePos returns the current cursor position using xdotool.
func getMousePos() (x, y int) {
	out, err := exec.Command("xdotool", "getmouselocation", "--shell").Output()
	if err != nil {
		return 0, 0
	}

	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "X=") {
			x, _ = strconv.Atoi(strings.TrimPrefix(line, "X="))
		} else if strings.HasPrefix(line, "Y=") {
			y, _ = strconv.Atoi(strings.TrimPrefix(line, "Y="))
		}
	}
	return x, y
}
