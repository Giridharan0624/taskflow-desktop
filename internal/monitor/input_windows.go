//go:build windows

package monitor

import (
	"sync/atomic"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	procGetKeyboardState = user32.NewProc("GetKeyboardState")
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
type InputTracker struct {
	keyboardTotal atomic.Uint32
	mouseTotal    atomic.Uint32
	lastCursorX   int32
	lastCursorY   int32
	lastKeyStates [256]bool
}

// NewInputTracker creates a new input tracker.
func NewInputTracker() *InputTracker {
	t := &InputTracker{}

	// Initialize cursor position
	var pt POINT
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	t.lastCursorX = pt.X
	t.lastCursorY = pt.Y

	return t
}

// GetCounts returns current keyboard and mouse event totals.
// Called every second from the activity tracker to compute deltas.
// Uses GetKeyboardState (reads state without consuming events) instead of
// GetAsyncKeyState (which clears the "pressed since last query" bit and
// can eat key events before the WebView processes them).
func (t *InputTracker) GetCounts() (keyboard uint32, mouse uint32) {
	// Read entire keyboard state in one call — does NOT consume events
	var keyStates [256]byte
	procGetKeyboardState.Call(uintptr(unsafe.Pointer(&keyStates[0])))

	keysPressed := 0
	for vk := 8; vk < 255; vk++ { // Skip mouse buttons (0x01-0x07)
		isDown := (keyStates[vk] & 0x80) != 0
		wasDown := t.lastKeyStates[vk]

		if isDown && !wasDown {
			keysPressed++
		}
		t.lastKeyStates[vk] = isDown
	}
	if keysPressed > 0 {
		t.keyboardTotal.Add(uint32(keysPressed))
	}

	// Check mouse: detect cursor movement
	var pt POINT
	ret, _, _ := procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	if ret != 0 {
		if pt.X != t.lastCursorX || pt.Y != t.lastCursorY {
			t.mouseTotal.Add(1)
			t.lastCursorX = pt.X
			t.lastCursorY = pt.Y
		}
	}

	// Check mouse buttons from keyboard state (high bit = pressed)
	if (keyStates[0x01] & 0x80) != 0 { // VK_LBUTTON
		t.mouseTotal.Add(1)
	}
	if (keyStates[0x02] & 0x80) != 0 { // VK_RBUTTON
		t.mouseTotal.Add(1)
	}

	return t.keyboardTotal.Load(), t.mouseTotal.Load()
}

// Ensure windows package is used (for the lazy DLLs defined in other files).
var _ = windows.ERROR_SUCCESS
