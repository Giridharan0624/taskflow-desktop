package monitor

import (
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
func (t *InputTracker) GetCounts() (keyboard uint32, mouse uint32) {
	// Check keyboard: poll all virtual key codes (1-254)
	// GetAsyncKeyState returns the key state — if the high bit is set, key is currently pressed
	keysPressed := 0
	for vk := 1; vk < 255; vk++ {
		ret, _, _ := procGetAsyncKeyState.Call(uintptr(vk))
		isDown := (ret & 0x8000) != 0
		wasDown := t.lastKeyStates[vk]

		// Count new key presses (transition from up to down)
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

	// Check mouse buttons (left=1, right=2, middle=4)
	for _, vk := range []uintptr{
		0x01, // VK_LBUTTON
		0x02, // VK_RBUTTON
		0x04, // VK_MBUTTON
	} {
		ret, _, _ := procGetAsyncKeyState.Call(vk)
		if (ret & 0x8000) != 0 {
			t.mouseTotal.Add(1)
		}
	}

	return t.keyboardTotal.Load(), t.mouseTotal.Load()
}

// Ensure windows package is used (for the lazy DLLs defined in other files).
var _ = windows.ERROR_SUCCESS
