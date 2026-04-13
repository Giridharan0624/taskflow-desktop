//go:build windows

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
// Uses GetAsyncKeyState because this polls from a background goroutine with
// no message pump — GetKeyboardState would return all zeros in that context.
func (t *InputTracker) GetCounts() (keyboard uint32, mouse uint32) {
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

	// Mouse movement via cursor position delta.
	var pt POINT
	ret, _, _ := procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	if ret != 0 {
		if pt.X != t.lastCursorX || pt.Y != t.lastCursorY {
			t.mouseTotal.Add(1)
			t.lastCursorX = pt.X
			t.lastCursorY = pt.Y
		}
	}

	return t.keyboardTotal.Load(), t.mouseTotal.Load()
}

// Ensure windows package is used (for the lazy DLLs defined in other files).
var _ = windows.ERROR_SUCCESS
