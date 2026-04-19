package main

import (
	"embed"
	"log"
	"os"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"

	"taskflow-desktop/internal/state"
)

//go:embed all:frontend/dist
var assets embed.FS

// logFileHandle holds the *os.File opened by setupLogging so shutdown
// can close it explicitly. Without this, the last log line can be lost
// on Windows (the file handle stays open but its buffer isn't flushed
// on process termination), and external log-rotation tools can't move
// the file while the app runs. See M-CORE-1.
var logFileHandle *os.File

// closeLogFile flushes and closes the log file handle, if one was
// successfully opened by setupLogging. Safe to call multiple times
// and safe on platforms / environments where logging falls back to
// stderr (handle stays nil).
func closeLogFile() {
	if logFileHandle == nil {
		return
	}
	// Restore log output to stderr before closing so any log calls
	// that race with shutdown don't write to a closed FD.
	log.SetOutput(os.Stderr)
	_ = logFileHandle.Close()
	logFileHandle = nil
}

func main() {
	// Prevent multiple instances (platform-specific)
	ensureSingleInstance()

	// Platform-specific log directory setup
	setupLogging()
	log.Println("=== TaskFlow Desktop starting ===")

	app := NewApp()

	appOptions := &options.App{
		Title:            "TaskFlow Desktop",
		Width:            450,
		Height:           500,
		MaxWidth:         450,
		MinWidth:         450,
		MaxHeight:        500,
		MinHeight:        500,
		DisableResize:    true,
		BackgroundColour: &options.RGBA{R: 255, G: 255, B: 255, A: 1},
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		OnStartup:     app.startup,
		OnShutdown:    app.shutdown,
		OnBeforeClose: app.beforeClose,
		Bind: []interface{}{
			app,
		},
	}

	// Apply platform-specific options (Windows, Linux, macOS)
	applyPlatformOptions(appOptions)

	if err := wails.Run(appOptions); err != nil {
		log.Fatalf("Error starting application: %v", err)
	}
}

// NewApp creates a new App instance with all services initialized.
func NewApp() *App {
	appState := state.New()
	return &App{
		State: appState,
	}
}
