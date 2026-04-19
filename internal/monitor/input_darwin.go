//go:build darwin

package monitor

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework CoreGraphics -framework AppKit
#include <CoreGraphics/CoreGraphics.h>
#include <AppKit/NSEvent.h>

// getKeyboardCount returns the system-wide count of key down events.
static unsigned long long getKeyboardCount() {
	return (unsigned long long)CGEventSourceCounterForEventType(
		kCGEventSourceStateCombinedSessionState,
		kCGEventKeyDown
	);
}

// getMouseMoveCount returns the system-wide count of mouse move events.
static unsigned long long getMouseMoveCount() {
	return (unsigned long long)CGEventSourceCounterForEventType(
		kCGEventSourceStateCombinedSessionState,
		kCGEventMouseMoved
	);
}

// getMouseClickCount returns the system-wide count of left mouse down events.
static unsigned long long getMouseClickCount() {
	return (unsigned long long)CGEventSourceCounterForEventType(
		kCGEventSourceStateCombinedSessionState,
		kCGEventLeftMouseDown
	);
}
*/
import "C"

import "sync/atomic"

// InputTracker tracks keyboard and mouse activity on macOS.
// Uses CGEventSource counters — system-wide event counts that don't require hooks.
// Requires Accessibility permission (System Preferences > Privacy > Accessibility).
type InputTracker struct {
	keyboardTotal atomic.Uint32
	mouseTotal    atomic.Uint32
	lastKbCount   uint64
	lastMoveCount uint64
	lastClickCount uint64
}

func NewInputTracker() *InputTracker {
	t := &InputTracker{}
	t.lastKbCount = uint64(C.getKeyboardCount())
	t.lastMoveCount = uint64(C.getMouseMoveCount())
	t.lastClickCount = uint64(C.getMouseClickCount())
	return t
}

// Reset zeroes the running totals and reseeds the baseline system-
// counter snapshot from the current CGEventSource values, so the
// next session starts at delta = 0 regardless of how many events the
// system counted while the tracker was stopped. See M-MON-1.
func (t *InputTracker) Reset() {
	t.keyboardTotal.Store(0)
	t.mouseTotal.Store(0)
	t.lastKbCount = uint64(C.getKeyboardCount())
	t.lastMoveCount = uint64(C.getMouseMoveCount())
	t.lastClickCount = uint64(C.getMouseClickCount())
}

// GetCounts returns current keyboard and mouse event totals.
func (t *InputTracker) GetCounts() (keyboard uint32, mouse uint32) {
	// Keyboard delta
	kbNow := uint64(C.getKeyboardCount())
	if kbNow > t.lastKbCount {
		t.keyboardTotal.Add(uint32(kbNow - t.lastKbCount))
	}
	t.lastKbCount = kbNow

	// Mouse delta (moves + clicks)
	moveNow := uint64(C.getMouseMoveCount())
	clickNow := uint64(C.getMouseClickCount())
	mouseDelta := uint32(0)
	if moveNow > t.lastMoveCount {
		mouseDelta += uint32(moveNow - t.lastMoveCount)
	}
	if clickNow > t.lastClickCount {
		mouseDelta += uint32(clickNow - t.lastClickCount)
	}
	if mouseDelta > 0 {
		t.mouseTotal.Add(mouseDelta)
	}
	t.lastMoveCount = moveNow
	t.lastClickCount = clickNow

	return t.keyboardTotal.Load(), t.mouseTotal.Load()
}
