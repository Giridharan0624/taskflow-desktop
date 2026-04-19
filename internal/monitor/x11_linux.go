//go:build linux

package monitor

import (
	"log"
	"sync"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/screensaver"
	"github.com/jezek/xgb/xproto"
)

// x11 holds a process-lifetime X server connection plus cached atoms and
// a probe of the MIT-SCREEN-SAVER extension. The previous Linux monitor
// forked xdotool/xprintidle 2–3 times per second; those subprocesses
// opened their own X connections, did one query, and exited. Keeping a
// single long-lived connection here does the same queries with zero
// process spawns and zero per-distro package hunt (no xdotool/xprintidle
// install required — any X11 session works out of the box).
//
// The connection is lazy: getX11() returns nil if there is no DISPLAY,
// no X server is reachable, or the XAUTHORITY cookie is rejected. Every
// helper below must tolerate nil and return the same zero value the old
// exec-based code returned on failure — callers rely on that contract.
type x11 struct {
	conn *xgb.Conn
	root xproto.Window

	// haveScreensaver is set when the MIT-SCREEN-SAVER extension
	// initialized. Pure-Wayland sessions without XWayland, or X servers
	// built without the extension (rare), leave this false and
	// GetIdleSeconds will return 0 — same as xprintidle's "not available"
	// failure mode.
	haveScreensaver bool

	// Cached EWMH atoms. Resolved lazily from atomName; a per-lookup
	// InternAtom round-trip would be wasteful since these never change
	// for the life of the connection.
	atomMu sync.Mutex
	atoms  map[string]xproto.Atom
}

var (
	x11Once sync.Once
	x11Mu   sync.RWMutex
	x11Inst *x11
)

// getX11 returns the shared X connection, or nil if X is unavailable.
// Callers MUST check for nil and degrade gracefully.
func getX11() *x11 {
	x11Mu.RLock()
	inst := x11Inst
	x11Mu.RUnlock()
	if inst != nil {
		return inst
	}
	x11Once.Do(func() {
		conn, err := xgb.NewConn()
		if err != nil {
			log.Printf("x11: no display available (%v) — window/idle/mouse tracking disabled", err)
			return
		}
		setup := xproto.Setup(conn)
		if setup == nil || len(setup.Roots) == 0 {
			log.Printf("x11: server returned no screens")
			conn.Close()
			return
		}
		inst := &x11{
			conn:  conn,
			root:  setup.Roots[0].Root,
			atoms: make(map[string]xproto.Atom),
		}
		// Probe the MIT-SCREEN-SAVER extension. Init() sends a QueryVersion
		// request; an error here means the extension isn't present and we
		// must fall back to "idle = 0" so callers can short-circuit the
		// idle heuristic on Wayland-only sessions.
		if err := screensaver.Init(conn); err != nil {
			log.Printf("x11: MIT-SCREEN-SAVER unavailable (%v) — idle detection disabled", err)
		} else {
			inst.haveScreensaver = true
		}
		x11Mu.Lock()
		x11Inst = inst
		x11Mu.Unlock()
	})
	x11Mu.RLock()
	defer x11Mu.RUnlock()
	return x11Inst
}

// CloseX11 closes the shared X connection and clears the atom cache.
// Safe to call multiple times and safe to call if X was never
// successfully initialized.
//
// Callers MUST ensure no goroutine is still making X calls when this
// returns — typically invoke it only AFTER ActivityMonitor.Stop has
// drained its goroutines. Any subsequent getX11() call will lazily
// reconnect (supports recovery paths like X server restart, though no
// current caller triggers that).
func CloseX11() {
	x11Mu.Lock()
	defer x11Mu.Unlock()
	if x11Inst == nil {
		return
	}
	x11Inst.conn.Close()
	x11Inst = nil
	// Reset sync.Once so future getX11() calls retry the connection
	// rather than reusing the nil cache.
	x11Once = sync.Once{}
}

// invalidateX11 drops the cached connection WITHOUT taking the lock —
// callers must already hold x11Mu. Used by health-check helpers to
// abandon a dead connection so the next getX11 reconnects.
func invalidateX11Locked() {
	if x11Inst == nil {
		return
	}
	x11Inst.conn.Close()
	x11Inst = nil
	x11Once = sync.Once{}
}

// atom resolves an EWMH atom name to its numeric ID, caching the result.
// Returns 0 on failure — callers that pass 0 to xproto.GetProperty will
// receive an X error which propagates as an empty reply, matching the
// pre-existing "window has no such property" behavior.
func (x *x11) atom(name string) xproto.Atom {
	x.atomMu.Lock()
	defer x.atomMu.Unlock()
	if a, ok := x.atoms[name]; ok {
		return a
	}
	reply, err := xproto.InternAtom(x.conn, false, uint16(len(name)), name).Reply()
	if err != nil || reply == nil {
		return 0
	}
	x.atoms[name] = reply.Atom
	return reply.Atom
}

// getIdleMs returns milliseconds since last user input via
// MIT-SCREEN-SAVER, or 0 if the extension is unavailable. This is
// byte-for-byte what `xprintidle` returns — the tool is literally a 30-
// line wrapper over the same extension.
//
// Also doubles as the X-server health check: this runs every second
// from the activity tick, so an error here is the fastest signal
// that the X server went away (crash, display hot-swap). On error we
// invalidate the shared connection so the NEXT getX11() reconnects.
// Without that, a dead connection would silently keep returning 0
// idle forever, inflating the keyboard heuristic's false-press rate
// until the next app restart. See V2-L2.
func (x *x11) getIdleMs() int {
	if x == nil || !x.haveScreensaver {
		return 0
	}
	info, err := screensaver.QueryInfo(x.conn, xproto.Drawable(x.root)).Reply()
	if err != nil || info == nil {
		// Drop the cache; next caller reconnects.
		x11Mu.Lock()
		if x11Inst == x {
			invalidateX11Locked()
		}
		x11Mu.Unlock()
		return 0
	}
	return int(info.MsSinceUserInput)
}

// getActiveWindow returns the currently-focused window ID per EWMH, or
// 0 if no _NET_ACTIVE_WINDOW atom is set (pre-EWMH WMs, some tiling WMs).
func (x *x11) getActiveWindow() xproto.Window {
	if x == nil {
		return 0
	}
	atom := x.atom("_NET_ACTIVE_WINDOW")
	if atom == 0 {
		return 0
	}
	reply, err := xproto.GetProperty(x.conn, false, x.root, atom, xproto.AtomWindow, 0, 1).Reply()
	if err != nil || reply == nil || len(reply.Value) < 4 {
		return 0
	}
	return xproto.Window(uint32(reply.Value[0]) |
		uint32(reply.Value[1])<<8 |
		uint32(reply.Value[2])<<16 |
		uint32(reply.Value[3])<<24)
}

// getWindowTitle reads _NET_WM_NAME (UTF-8), falling back to legacy
// WM_NAME (Latin-1) so pre-EWMH apps (~2005 and earlier) still report a
// title. xdotool had this same two-step fallback.
func (x *x11) getWindowTitle(w xproto.Window) string {
	if x == nil || w == 0 {
		return ""
	}
	if name := x.readStringProperty(w, x.atom("_NET_WM_NAME"), x.atom("UTF8_STRING")); name != "" {
		return name
	}
	return x.readStringProperty(w, xproto.AtomWmName, xproto.AtomString)
}

// getWindowPID reads _NET_WM_PID. Returns 0 if unset (apps that don't
// cooperate with EWMH — rare, but e.g. some Java AWT windows don't set
// it). Caller falls back to title in that case.
func (x *x11) getWindowPID(w xproto.Window) int {
	if x == nil || w == 0 {
		return 0
	}
	atom := x.atom("_NET_WM_PID")
	if atom == 0 {
		return 0
	}
	reply, err := xproto.GetProperty(x.conn, false, w, atom, xproto.AtomCardinal, 0, 1).Reply()
	if err != nil || reply == nil || len(reply.Value) < 4 {
		return 0
	}
	return int(uint32(reply.Value[0]) |
		uint32(reply.Value[1])<<8 |
		uint32(reply.Value[2])<<16 |
		uint32(reply.Value[3])<<24)
}

// readStringProperty fetches up to 1 KiB of a string-typed window
// property. The 1 KiB cap matches xdotool's behavior and prevents a
// pathological window title from blowing memory.
func (x *x11) readStringProperty(w xproto.Window, prop, typ xproto.Atom) string {
	if prop == 0 {
		return ""
	}
	reply, err := xproto.GetProperty(x.conn, false, w, prop, typ, 0, 256).Reply()
	if err != nil || reply == nil || len(reply.Value) == 0 {
		return ""
	}
	return string(reply.Value)
}

// getMousePos returns the root-relative cursor position via
// QueryPointer. Returns (0,0) if X is unavailable — same as xdotool's
// failure mode, and InputTracker treats (0,0) as "no movement this
// tick" on the initial read anyway.
func (x *x11) getMousePos() (int, int) {
	if x == nil {
		return 0, 0
	}
	reply, err := xproto.QueryPointer(x.conn, x.root).Reply()
	if err != nil || reply == nil {
		return 0, 0
	}
	return int(reply.RootX), int(reply.RootY)
}
