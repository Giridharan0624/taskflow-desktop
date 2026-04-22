//go:build windows

package monitor

import (
	"sync"
	"sync/atomic"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	procGetAsyncKeyState = user32.NewProc("GetAsyncKeyState")
	procGetCursorPos     = user32.NewProc("GetCursorPos")
)

// POINT is the Windows API POINT structure.
type POINT struct {
	X int32
	Y int32
}

// InputTracker tracks keyboard and mouse activity counts using Win32 polling.
// This approach avoids global hooks (which require CGO) and instead polls
// input state every second — sufficient for activity counting.
//
// mu guards lastCursor* and lastKeyStates so GetCounts is race-detector
// clean. Today it's only called from the activityMonitor tick (a single
// goroutine), so contention is effectively zero; tomorrow if anything
// wants to read the current cursor delta outside the tick, the mutex is
// already in place. See H-MON-4.
type InputTracker struct {
	keyboardTotal atomic.Uint32
	mouseTotal    atomic.Uint32

	mu            sync.Mutex
	lastCursorX   int32
	lastCursorY   int32
	// cursorSeeded is false until the first successful GetCursorPos.
	// Without this, a user whose cursor genuinely sits at (0,0) for
	// the whole session never registers mouse movement — the delta
	// check compares current (0,0) against the same initial (0,0)
	// value. Seeding tracks "did we ever read a real position"
	// instead of relying on the zero value. See V3-M1.
	cursorSeeded  bool
	lastKeyStates [256]bool
}

// NewInputTracker creates a new input tracker.
func NewInputTracker() *InputTracker {
	t := &InputTracker{}

	// Initialize cursor position. Only mark the baseline as seeded if
	// the call actually succeeded — a failed GetCursorPos leaves pt
	// zero-valued, which would be indistinguishable from a real
	// (0,0) sample.
	var pt POINT
	ret, _, _ := procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	t.mu.Lock()
	if ret != 0 {
		t.lastCursorX = pt.X
		t.lastCursorY = pt.Y
		t.cursorSeeded = true
	}
	t.mu.Unlock()

	return t
}

// Reset zeroes the running keyboard/mouse totals and resyncs cursor +
// keystate baselines to the current OS state. Called from
// ActivityMonitor.Stop so the next session starts from a clean zero
// and the first heartbeat doesn't report a historical accumulation.
// See M-MON-1.
func (t *InputTracker) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.keyboardTotal.Store(0)
	t.mouseTotal.Store(0)

	// Reseed cursor baseline; unseed on failure so the first real
	// sample after reset is counted as movement. See V3-M1.
	var pt POINT
	ret, _, _ := procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	if ret != 0 {
		t.lastCursorX = pt.X
		t.lastCursorY = pt.Y
		t.cursorSeeded = true
	} else {
		t.cursorSeeded = false
	}

	// Reseed key states — without this, a key held down across the
	// Reset boundary would look newly-pressed on the next GetCounts
	// tick and inflate the counter by one.
	for vk := 0x01; vk < 0xFF; vk++ {
		ret, _, _ := procGetAsyncKeyState.Call(uintptr(vk))
		t.lastKeyStates[vk] = (ret & 0x8000) != 0
	}
}

// GetCounts returns current keyboard and mouse event totals.
// Uses GetAsyncKeyState because this polls from a background goroutine with
// no message pump — GetKeyboardState would return all zeros in that context.
func (t *InputTracker) GetCounts() (keyboard uint32, mouse uint32) {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Mouse buttons: VK_LBUTTON=0x01, VK_RBUTTON=0x02, VK_MBUTTON=0x04, VK_XBUTTON1=0x05, VK_XBUTTON2=0x06.
	for _, vk := range [...]int{0x01, 0x02, 0x04, 0x05, 0x06} {
		ret, _, _ := procGetAsyncKeyState.Call(uintptr(vk))
		isDown := (ret & 0x8000) != 0
		if isDown && !t.lastKeyStates[vk] {
			t.mouseTotal.Add(1)
		}
		t.lastKeyStates[vk] = isDown
	}

	// Keyboard keys 0x08..0xFE (skips the mouse button range above).
	keysPressed := 0
	for vk := 0x08; vk < 0xFF; vk++ {
		ret, _, _ := procGetAsyncKeyState.Call(uintptr(vk))
		isDown := (ret & 0x8000) != 0
		if isDown && !t.lastKeyStates[vk] {
			keysPressed++
		}
		t.lastKeyStates[vk] = isDown
	}
	if keysPressed > 0 {
		t.keyboardTotal.Add(uint32(keysPressed))
	}

	// Mouse movement via cursor position delta. The first successful
	// sample after an unseeded reset seeds the baseline WITHOUT
	// counting a movement — otherwise every Reset() would inject one
	// phantom event.
	var pt POINT
	ret, _, _ := procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	if ret != 0 {
		if !t.cursorSeeded {
			t.lastCursorX = pt.X
			t.lastCursorY = pt.Y
			t.cursorSeeded = true
		} else if pt.X != t.lastCursorX || pt.Y != t.lastCursorY {
			t.mouseTotal.Add(1)
			t.lastCursorX = pt.X
			t.lastCursorY = pt.Y
		}
	}

	return t.keyboardTotal.Load(), t.mouseTotal.Load()
}

// Ensure windows package is used (for the lazy DLLs defined in other files).
var _ = windows.ERROR_SUCCESS
