//go:build linux

package monitor

import (
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

// InputTracker tracks keyboard and mouse activity on Linux.
// Uses xdotool for mouse position polling. Keyboard counting uses the
// idle-time heuristic (if the desktop reports <2s of idle, something
// was pressed).
//
// idle is cached as a struct field instead of being allocated each
// GetCounts tick. Previously NewIdleDetector() forked xprintidle twice
// per second (once here, once in trackActivity), burning CPU on every
// poll and — on Wayland-without-xprintidle sessions — permanently
// reading "0 idle" which made every tick count a false keyboard press.
// See H-MON-2.
type InputTracker struct {
	keyboardTotal atomic.Uint32
	mouseTotal    atomic.Uint32

	mu          sync.Mutex
	lastCursorX int
	lastCursorY int
	idle        *IdleDetector
}

func NewInputTracker() *InputTracker {
	t := &InputTracker{
		idle: NewIdleDetector(),
	}

	// Initialize cursor position
	x, y := getMousePos()
	t.lastCursorX = x
	t.lastCursorY = y

	return t
}

// GetCounts returns current keyboard and mouse event totals.
func (t *InputTracker) GetCounts() (keyboard uint32, mouse uint32) {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Mouse: detect cursor movement via xdotool
	x, y := getMousePos()
	if x != t.lastCursorX || y != t.lastCursorY {
		t.mouseTotal.Add(1)
		t.lastCursorX = x
		t.lastCursorY = y
	}

	// Keyboard: xdotool doesn't expose key events without global hooks,
	// so we fall back to the idle-time heuristic. Reusing t.idle (rather
	// than allocating per tick) avoids forking xprintidle twice per
	// second.
	if t.idle != nil {
		if idleSec := t.idle.GetIdleSeconds(); idleSec < 2 {
			t.keyboardTotal.Add(1)
		}
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
