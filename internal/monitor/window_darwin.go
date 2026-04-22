//go:build darwin

package monitor

import (
	"context"
	"log"
	"os/exec"
	"strings"
	"sync/atomic"
	"time"
)

// WindowTracker tracks the active foreground window on macOS.
// Uses osascript (AppleScript) to get the frontmost application name.
//
// osascript is called with a 500 ms timeout via exec.CommandContext.
// If it hangs (pending AppleScript dialog, Accessibility prompt) we
// return the last good value instead of stalling the 5-second window
// tick. See V3-H2.
type WindowTracker struct {
	lastName atomic.Value // string
}

func NewWindowTracker() *WindowTracker {
	w := &WindowTracker{}
	w.lastName.Store("")
	return w
}

const osascriptTimeout = 500 * time.Millisecond

// GetActiveWindowApp returns the friendly name of the foreground application.
func (w *WindowTracker) GetActiveWindowApp() string {
	ctx, cancel := context.WithTimeout(context.Background(), osascriptTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "osascript", "-e",
		`tell application "System Events" to get name of first application process whose frontmost is true`,
	).Output()
	if err != nil {
		if ctx.Err() != nil {
			log.Printf("window: osascript timed out after %s — returning cached value", osascriptTimeout)
		}
		if cached, _ := w.lastName.Load().(string); cached != "" {
			return cached
		}
		return ""
	}

	name := strings.TrimSpace(string(out))
	if name == "" {
		if cached, _ := w.lastName.Load().(string); cached != "" {
			return cached
		}
		return ""
	}

	friendly := friendlyAppName(name)
	w.lastName.Store(friendly)
	return friendly
}
