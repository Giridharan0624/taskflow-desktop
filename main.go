package main

import (
	"embed"
	"log"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"

	"taskflow-desktop/internal/state"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
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
