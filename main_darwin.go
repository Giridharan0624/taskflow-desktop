//go:build darwin

package main

import (
	"log"
	"os"
	"path/filepath"

	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
)

func ensureSingleInstance() {
	// macOS apps are single-instance by default via NSApplication
}

func setupLogging() {
	home, _ := os.UserHomeDir()
	logDir := filepath.Join(home, "Library", "Application Support", "TaskFlow")
	os.MkdirAll(logDir, 0755)
	logFile, err := os.OpenFile(filepath.Join(logDir, "taskflow.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err == nil {
		log.SetOutput(logFile)
	}
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
