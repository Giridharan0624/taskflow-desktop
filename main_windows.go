//go:build windows

package main

import (
	"log"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"

	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
)

// ensureSingleInstance uses a Windows named mutex to prevent multiple instances.
// If another instance is already running, this shows a message and exits.
func ensureSingleInstance() {
	name, _ := syscall.UTF16PtrFromString("Global\\TaskFlowDesktop_SingleInstance")
	handle, _, err := syscall.NewLazyDLL("kernel32.dll").NewProc("CreateMutexW").Call(
		0,
		0,
		uintptr(unsafe.Pointer(name)),
	)

	const ERROR_ALREADY_EXISTS = 183
	if handle == 0 || err == syscall.Errno(ERROR_ALREADY_EXISTS) {
		// Another instance is running — show message and exit
		msg, _ := syscall.UTF16PtrFromString("TaskFlow Desktop is already running.\nCheck your system tray.")
		title, _ := syscall.UTF16PtrFromString("TaskFlow Desktop")
		syscall.NewLazyDLL("user32.dll").NewProc("MessageBoxW").Call(
			0,
			uintptr(unsafe.Pointer(msg)),
			uintptr(unsafe.Pointer(title)),
			0x00000040, // MB_ICONINFORMATION
		)
		os.Exit(0)
	}
	// Mutex handle intentionally not closed — stays alive for the process lifetime
}

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
