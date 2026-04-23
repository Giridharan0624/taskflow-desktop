//go:build windows

package monitor

import (
	"syscall"
	"unsafe"
)

// Windows exposes several signals that could answer "is the screen
// locked" but they vary in reliability across versions:
//
//   1. WTSQuerySessionInformationW(WTSSessionInfoEx) + SessionFlags —
//      the "official" API, but on Windows 11 the SessionFlags field
//      is frequently uninitialized (returns 0 == WTS_SESSIONSTATE_LOCK)
//      during normal active use, producing false positives. This is
//      the path the previous implementation used; we kept the struct
//      definitions below as a fallback but no longer trust the value.
//
//   2. OpenInputDesktop → GetUserObjectInformationW (name) — when
//      the workstation is locked, the active input desktop is
//      "Winlogon" (the secure desktop); when unlocked it is
//      "Default". A failure to open the desktop at all also
//      indicates lock (ERROR_ACCESS_DENIED from a user-session
//      call to the secure desktop). This is what tools like
//      LogonExpert, ScreenCapture, and the standard
//      stackoverflow.com/q/2213606 answer recommend.
//
// We use (2) as the primary signal and fall through to idle-proxy
// detection (see screenshot.go) only when OpenInputDesktop itself
// fails with a non-permission error.

var (
	user32DLL                      = syscall.NewLazyDLL("user32.dll")
	procOpenInputDesktop           = user32DLL.NewProc("OpenInputDesktop")
	procCloseDesktop               = user32DLL.NewProc("CloseDesktop")
	procGetUserObjectInformationW  = user32DLL.NewProc("GetUserObjectInformationW")
)

const (
	desktopReadObjects = 0x0001
	uoiName            = 2
)

// nativeIsScreenLocked reports whether the Windows workstation is
// currently locked.
//
// Returns (locked, ok):
//   - (true, true)  — definitely locked
//   - (false, true) — definitely unlocked
//   - (_, false)    — couldn't tell; caller should fall back to
//                     idle-proxy heuristic (>10 min idle ⇒ likely
//                     locked)
//
// Uses OpenInputDesktop + GetUserObjectInformationW to read the
// active input desktop's name. "Default" means the user's normal
// interactive desktop; anything else (most commonly "Winlogon") or
// a NULL handle means a secure desktop is active (Lock screen,
// login prompt, UAC). This is more reliable than the WTS
// SessionFlags API, which returns spurious "locked" on fresh-login
// Windows 11 sessions. See V3-Wlock.
func nativeIsScreenLocked() (bool, bool) {
	// OpenInputDesktop(dwFlags=0, fInherit=0, dwDesiredAccess)
	hDesktop, _, err := procOpenInputDesktop.Call(
		0,
		0,
		uintptr(desktopReadObjects),
	)
	if hDesktop == 0 {
		// ERROR_ACCESS_DENIED (5) is the textbook locked signal:
		// the secure desktop is active, and our user-session call
		// is rejected. Any other failure means "we don't know" —
		// fall through to the idle-proxy fallback rather than
		// reporting a false lock and breaking captures.
		if errno, ok := err.(syscall.Errno); ok && errno == syscall.ERROR_ACCESS_DENIED {
			return true, true
		}
		return false, false
	}
	defer procCloseDesktop.Call(hDesktop)

	// Fetch the desktop name. A 64-char buffer is overkill for the
	// names we care about ("Default", "Winlogon", "Screen-saver")
	// but cheap.
	var nameBuf [64]uint16
	var needed uint32
	ret, _, _ := procGetUserObjectInformationW.Call(
		hDesktop,
		uintptr(uoiName),
		uintptr(unsafe.Pointer(&nameBuf[0])),
		uintptr(len(nameBuf)*2), // bytes, not chars
		uintptr(unsafe.Pointer(&needed)),
	)
	if ret == 0 {
		// Couldn't read the name — don't assert lock/unlock.
		return false, false
	}

	name := syscall.UTF16ToString(nameBuf[:])
	// "Default" is the ONLY name that means the user's normal
	// interactive desktop is the active input desktop. Every other
	// name (Winlogon, Screen-saver, Disconnect) indicates a secure
	// or otherwise non-interactive desktop is in the foreground.
	if name == "Default" {
		return false, true
	}
	return true, true
}

// WTS_* structs and constants kept for reference and possible
// future use; not called from nativeIsScreenLocked. Leaving them
// here rather than deleting because they document what the
// previous implementation tried and why we abandoned it.
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
	Data  wtsInfoExLevel1W
}

var (
	_ = syscall.NewLazyDLL("wtsapi32.dll")
	_ = &wtsInfoExW{}
)
