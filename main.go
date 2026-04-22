package main

import (
	"embed"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"

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

	// Catch SIGINT (Ctrl-C) and SIGTERM (systemctl stop, pkill,
	// session-manager logout) so we get a chance to auto-sign-out AND
	// drain background services (activity monitor, tray) before the
	// process dies. Wails handles Windows WM_QUERYENDSESSION /
	// WM_ENDSESSION through OnShutdown on its own; this hook is the
	// Unix equivalent.
	//
	// We MUST NOT call os.Exit(0) directly here — it bypasses Wails'
	// OnShutdown, which means ActivityMonitor.Stop() never flushes
	// the in-memory activity bucket (up to 5 min of keyboard/mouse
	// counters silently lost) and TrayManager.Stop() never runs
	// (leaving a ghost icon on Windows). Instead we auto-sign-out,
	// then call runtime.Quit which runs OnShutdown and exits
	// gracefully. A watchdog goroutine is the fallback for the
	// genuinely-wedged case. See V3-C2.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		log.Printf("received %s — auto-signing-out and requesting graceful shutdown", sig)
		app.autoSignOutIfRunning(3 * time.Second)
		if ctx := app.ctx; ctx != nil {
			wailsruntime.Quit(ctx)
			// Hard watchdog: if Wails doesn't tear down within 5 s
			// the runtime is wedged and the user would otherwise
			// see a zombie process. Exit with code 0 so systemd /
			// service managers don't treat it as a crash.
			go func() {
				time.Sleep(5 * time.Second)
				log.Println("watchdog: Wails runtime did not exit within 5s after quit request — force-exiting")
				closeLogFile()
				os.Exit(0)
			}()
			return
		}
		// Wails never finished bootstrapping (ctx nil) — fall through
		// to direct exit so a stuck startup still honors the signal.
		closeLogFile()
		os.Exit(0)
	}()

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
