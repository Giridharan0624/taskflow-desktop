//go:build linux

package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"syscall"

	"github.com/wailsapp/wails/v2/pkg/options"
)

// singleInstanceLock holds the exclusive flock for the process lifetime.
// Keeping the *os.File at package scope prevents GC from closing the
// file and dropping the lock. See C-CORE-3.
var singleInstanceLock *os.File

// ensureSingleInstance takes an advisory exclusive flock on a lockfile
// in the user's XDG data directory. The first instance holds it until
// the process exits; subsequent instances get EAGAIN/EWOULDBLOCK, print
// a friendly message, and exit cleanly. See C-CORE-3 / T-PLATFORM-1.
func ensureSingleInstance() {
	dataDir := os.Getenv("XDG_DATA_HOME")
	if dataDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return // without a home dir we can't lock; fall through
		}
		dataDir = filepath.Join(home, ".local", "share")
	}
	lockDir := filepath.Join(dataDir, "TaskFlow")
	if err := os.MkdirAll(lockDir, 0700); err != nil {
		return
	}
	lockPath := filepath.Join(lockDir, "app.lock")

	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		fmt.Fprintln(os.Stderr, "TaskFlow Desktop is already running.")
		_ = f.Close()
		os.Exit(0)
	}
	// Hold the file (and thus the lock) for the process lifetime.
	singleInstanceLock = f
}

func setupLogging() {
	// XDG_DATA_HOME or ~/.local/share
	dataDir := os.Getenv("XDG_DATA_HOME")
	if dataDir == "" {
		home, _ := os.UserHomeDir()
		dataDir = filepath.Join(home, ".local", "share")
	}
	logDir := filepath.Join(dataDir, "TaskFlow")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		// Log dir unreachable; stderr is already the default sink.
		log.Printf("setupLogging: mkdir %q failed: %v — logging to stderr", logDir, err)
		return
	}
	f, err := os.OpenFile(filepath.Join(logDir, "taskflow.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		log.Printf("setupLogging: open log file failed: %v — logging to stderr", err)
		return
	}
	logFileHandle = f
	log.SetOutput(f)
}

func applyPlatformOptions(opts *options.App) {
	// Linux uses webkit2gtk — no special options needed
}
