//go:build linux

package monitor

import (
	"sync"
	"sync/atomic"
)

// InputTracker counts keyboard and mouse activity on Linux.
//
// Mouse: previously shelled out to `xdotool getmouselocation --shell`
// every tick. We now call XQueryPointer directly on the shared X
// connection — same cursor coordinates, no process spawn.
//
// Keyboard: X11 has no portable per-key hook without global grabs, so
// we keep the idle-time heuristic — if the desktop reports <2s since
// last input, the user touched *something*. This is identical to the
// previous behavior; only the data source underneath (now MIT-SCREEN-
// SAVER via x11_linux.go) changed.
//
// idle is cached as a struct field instead of being allocated per
// GetCounts tick. That matters because NewIdleDetector() used to fork
// xprintidle — doing it per tick burned CPU and produced false
// keyboard presses when xprintidle was missing. See H-MON-2.
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
	x, y := getX11().getMousePos()
	t.lastCursorX = x
	t.lastCursorY = y
	return t
}

// GetCounts returns current keyboard and mouse event totals.
func (t *InputTracker) GetCounts() (keyboard uint32, mouse uint32) {
	t.mu.Lock()
	defer t.mu.Unlock()

	x, y := getX11().getMousePos()
	if x != t.lastCursorX || y != t.lastCursorY {
		t.mouseTotal.Add(1)
		t.lastCursorX = x
		t.lastCursorY = y
	}

	if t.idle != nil {
		if idleSec := t.idle.GetIdleSeconds(); idleSec < 2 {
			t.keyboardTotal.Add(1)
		}
	}

	return t.keyboardTotal.Load(), t.mouseTotal.Load()
}
