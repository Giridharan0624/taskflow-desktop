//go:build linux

package main

import (
	"log"
	"os"
	"path/filepath"

	"github.com/wailsapp/wails/v2/pkg/options"
)

func ensureSingleInstance() {
	// TODO: Use flock on a lockfile for Linux single-instance check
}

func setupLogging() {
	// XDG_DATA_HOME or ~/.local/share
	dataDir := os.Getenv("XDG_DATA_HOME")
	if dataDir == "" {
		home, _ := os.UserHomeDir()
		dataDir = filepath.Join(home, ".local", "share")
	}
	logDir := filepath.Join(dataDir, "TaskFlow")
	os.MkdirAll(logDir, 0755)
	logFile, err := os.OpenFile(filepath.Join(logDir, "taskflow.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err == nil {
		log.SetOutput(logFile)
	}
}

func applyPlatformOptions(opts *options.App) {
	// Linux uses webkit2gtk — no special options needed
}
