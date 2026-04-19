//go:build darwin

package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"syscall"

	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
)

// singleInstanceLock holds the exclusive flock for the process lifetime.
// Keeping the *os.File at package scope prevents GC from closing the
// file and dropping the lock. See C-CORE-3.
var singleInstanceLock *os.File

// ensureSingleInstance takes an advisory exclusive flock on a lockfile
// in ~/Library/Application Support. NSApplication's "single-instance
// for bundle launches" guarantee doesn't cover binaries launched from
// the shell (wails dev, CLI-invoked .app), so we need this regardless.
// See C-CORE-3 / T-PLATFORM-1.
func ensureSingleInstance() {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	lockDir := filepath.Join(home, "Library", "Application Support", "TaskFlow")
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
	singleInstanceLock = f
}

func setupLogging() {
	home, _ := os.UserHomeDir()
	logDir := filepath.Join(home, "Library", "Application Support", "TaskFlow")
	if err := os.MkdirAll(logDir, 0755); err != nil {
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
	opts.Mac = &mac.Options{
		TitleBar: mac.TitleBarDefault(),
		About: &mac.AboutInfo{
			Title:   "TaskFlow Desktop",
			Message: "Time Tracker & Activity Monitor\nBy NEUROSTACK",
		},
	}
}
