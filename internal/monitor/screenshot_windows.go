//go:build windows

package monitor

import (
	"syscall"
	"unsafe"
)

// WTSINFOEX_LEVEL1_W layout from wtsapi32.h. We only care about
// SessionFlags; the rest is dead weight but has to be sized correctly
// so WTSQuerySessionInformation writes into its allocated memory.
// Total struct is 320 bytes in the Level-1 variant.
type wtsInfoExLevel1W struct {
	SessionState       int32
	SessionFlags       int32
	WinStationName     [32]uint16
	UserName           [21]uint16
	DomainName         [18]uint16
	LogonTime          int64
	ConnectTime        int64
	DisconnectTime     int64
	LastInputTime      int64
	CurrentTime        int64
	IncomingBytes      uint32
	OutgoingBytes      uint32
	IncomingFrames     uint32
	OutgoingFrames     uint32
	IncomingCompressed uint32
	OutgoingCompressed uint32
}

type wtsInfoExW struct {
	Level uint32
	Data  wtsInfoExLevel1W // union with level discriminator Level
}

var (
	wtsapi32               = syscall.NewLazyDLL("wtsapi32.dll")
	procWTSQuerySessionInfoExW = wtsapi32.NewProc("WTSQuerySessionInformationW")
	procWTSFreeMemory      = wtsapi32.NewProc("WTSFreeMemory")
)

const (
	wtsCurrentServerHandle  = 0
	wtsCurrentSession       = uint32(0xFFFFFFFF) // WTS_CURRENT_SESSION
	wtsSessionInfoEx        = uint32(25)         // WTSSessionInfoEx
	wtsSessionStateLock     = int32(0)           // WTS_SESSIONSTATE_LOCK
	wtsSessionStateUnlock   = int32(1)           // WTS_SESSIONSTATE_UNLOCK
)

// nativeIsScreenLocked uses WTSQuerySessionInformation(WTSSessionInfoEx)
// to read the current session's SessionFlags. Returns (locked, true) on
// success, (_, false) if the API is unreachable — caller falls back to
// the idle proxy. Windows 10+ only; older Windows had the flag values
// swapped (documented MS bug on Win7), which we accept as out of scope.
func nativeIsScreenLocked() (bool, bool) {
	// Typing buf as *wtsInfoExW directly avoids the uintptr-hop that
	// go vet (correctly) flags as a GC hazard — the OS-allocated
	// memory stays alive from allocation through WTSFreeMemory under
	// this type, and we never cast uintptr back to a pointer.
	var buf *wtsInfoExW
	var bytes uint32

	// BOOL WTSQuerySessionInformationW(HANDLE, DWORD, WTS_INFO_CLASS, LPWSTR*, DWORD*)
	ret, _, _ := procWTSQuerySessionInfoExW.Call(
		uintptr(wtsCurrentServerHandle),
		uintptr(wtsCurrentSession),
		uintptr(wtsSessionInfoEx),
		uintptr(unsafe.Pointer(&buf)),
		uintptr(unsafe.Pointer(&bytes)),
	)
	if ret == 0 || buf == nil {
		return false, false
	}
	defer procWTSFreeMemory.Call(uintptr(unsafe.Pointer(buf)))

	if buf.Level != 1 {
		return false, false
	}
	return buf.Data.SessionFlags == wtsSessionStateLock, true
}
