package main

import (
	"embed"
	"log"
	"os"
	"path/filepath"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"

	"taskflow-desktop/internal/state"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	// File logging so we can debug production builds
	logDir := filepath.Join(os.Getenv("APPDATA"), "TaskFlow")
	os.MkdirAll(logDir, 0755)
	logFile, err := os.OpenFile(filepath.Join(logDir, "taskflow.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err == nil {
		log.SetOutput(logFile)
	}
	log.Println("=== TaskFlow Desktop starting ===")

	app := NewApp()

	err = wails.Run(&options.App{
		Title:     "TaskFlow Desktop",
		Width:     450,
		Height:    500,
		MaxWidth:  450,
		MinWidth:  450,
		MaxHeight: 500,
		MinHeight: 500,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 255, G: 255, B: 255, A: 1},
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		OnBeforeClose:    app.beforeClose,
		DisableResize: true,
		Bind: []interface{}{
			app,
		},
		Windows: &windows.Options{
			WebviewIsTransparent:              false,
			WindowIsTranslucent:               false,
			DisableWindowIcon:                 false,
			DisableFramelessWindowDecorations: false,
			Theme:                             windows.SystemDefault,
		},
	})

	if err != nil {
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
