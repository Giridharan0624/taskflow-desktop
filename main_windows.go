//go:build windows

package main

import (
	"log"
	"os"
	"path/filepath"

	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
)

func setupLogging() {
	logDir := filepath.Join(os.Getenv("APPDATA"), "TaskFlow")
	os.MkdirAll(logDir, 0755)
	logFile, err := os.OpenFile(filepath.Join(logDir, "taskflow.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err == nil {
		log.SetOutput(logFile)
	}
}

func applyPlatformOptions(opts *options.App) {
	opts.Windows = &windows.Options{
		WebviewIsTransparent:              false,
		WindowIsTranslucent:               false,
		DisableWindowIcon:                 false,
		DisableFramelessWindowDecorations: false,
		Theme:                             windows.SystemDefault,
	}
}
