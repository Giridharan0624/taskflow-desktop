package monitor

import (
	"unsafe"
)

var (
	procGetLastInputInfo = user32.NewProc("GetLastInputInfo")
	procGetTickCount     = kernel32.NewProc("GetTickCount")
)

// LASTINPUTINFO is the Windows API structure for GetLastInputInfo.
type LASTINPUTINFO struct {
	CbSize uint32
	DwTime uint32
}

// IdleDetector detects how long the user has been idle (no keyboard/mouse input).
type IdleDetector struct{}

// NewIdleDetector creates a new idle detector.
func NewIdleDetector() *IdleDetector {
	return &IdleDetector{}
}

// GetIdleSeconds returns the number of seconds since the last keyboard/mouse input.
// Uses the Win32 GetLastInputInfo API for system-level accuracy.
func (d *IdleDetector) GetIdleSeconds() int {
	var lii LASTINPUTINFO
	lii.CbSize = uint32(unsafe.Sizeof(lii))

	ret, _, _ := procGetLastInputInfo.Call(uintptr(unsafe.Pointer(&lii)))
	if ret == 0 {
		return 0
	}

	tickCount, _, _ := procGetTickCount.Call()
	idleMs := uint32(tickCount) - lii.DwTime

	return int(idleMs / 1000)
}

// IsIdle returns true if the user has been idle for more than the given threshold (seconds).
func (d *IdleDetector) IsIdle(thresholdSeconds int) bool {
	return d.GetIdleSeconds() > thresholdSeconds
}
