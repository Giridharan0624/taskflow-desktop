package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"taskflow-desktop/internal/api"
	"taskflow-desktop/internal/auth"
	"taskflow-desktop/internal/monitor"
	"taskflow-desktop/internal/state"
	"taskflow-desktop/internal/tray"
	"taskflow-desktop/internal/updater"
)

// App is the main application struct. Methods bound to the frontend via Wails.
type App struct {
	ctx      context.Context // The EXACT Wails context — required for runtime calls
	trayStop chan struct{}   // App-lifetime stop signal for the tray message loop

	// sessionMu guards the session-scoped fields below. A "session" spans from
	// login to logout; each login creates a fresh context so re-login cleanly
	// replaces the previous session's background goroutines instead of racing
	// them.
	sessionMu     sync.Mutex
	sessionCtx    context.Context
	sessionCancel context.CancelFunc
	sessionWG     sync.WaitGroup

	quitting          bool // True when user clicks Quit from tray
	networkErrorCount int  // Consecutive network errors
	State             *state.AppState
	AuthService       *auth.Service
	APIClient         *api.Client
	ActivityMonitor   *monitor.ActivityMonitor
	TrayManager       *tray.Manager
}

// startup is called when the app starts. The context is saved for runtime calls.
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx // Store the EXACT Wails context — never wrap or replace it
	a.trayStop = make(chan struct{})

	a.AuthService = auth.NewService(a.State)
	a.APIClient = api.NewClient(a.AuthService, a.State)
	a.ActivityMonitor = monitor.NewActivityMonitor(a.APIClient, a.State)

	a.TrayManager = tray.NewManager(a.State)

	// Wire screenshot notifications to tray balloon
	a.ActivityMonitor.SetNotifyFunc(func(title, message string) {
		a.TrayManager.ShowBalloon(title, message)
	})
	a.TrayManager.SetHandler(&tray.ActionHandler{
		OnShowWindow: func() { a.ShowWindow() },
		OnStopTimer: func() {
			if _, err := a.SignOut(); err != nil {
				log.Printf("Tray: stop timer failed: %v", err)
			}
		},
		OnQuit: func() {
			go func() {
				log.Println("Quit requested from tray")
				// Auto-stop timer before quitting so backend doesn't stay SIGNED_IN
				if a.State.IsTimerActive() {
					log.Println("Timer active — signing out before quit...")
					if _, err := a.APIClient.SignOut(); err != nil {
						log.Printf("Auto sign-out failed: %v", err)
					} else {
						log.Println("Timer stopped successfully")
					}
				}
				a.quitting = true
				a.ActivityMonitor.Stop()
				a.TrayManager.Stop()
				runtime.Quit(a.ctx)
			}()
		},
	})

	if err := a.AuthService.TryRestoreSession(); err != nil {
		log.Printf("No stored session found: %v", err)
	} else {
		log.Println("Session restored from stored tokens")
		a.State.SetAuthenticated(true)
		a.startBackgroundServices()
	}

	// Start system tray (pure Win32 — no library conflict with Wails).
	// Uses trayStop (app-lifetime) rather than the session context, since the
	// tray must persist across logout/re-login.
	go a.TrayManager.Start(a.trayStop)

	// Check for updates silently on startup
	go func() {
		time.Sleep(5 * time.Second) // Wait for app to fully load
		info, err := updater.CheckForUpdate()
		if err != nil {
			log.Printf("Update check failed: %v", err)
			return
		}
		if info.Available {
			log.Printf("Update available: v%s (current: v%s)", info.Version, info.CurrentVer)
			runtime.EventsEmit(a.ctx, "update:available", info)
		}
	}()
}

// shutdown is called when the app is closing.
func (a *App) shutdown(ctx context.Context) {
	a.stopBackgroundServices()
	a.ActivityMonitor.Stop()
	// Close the tray signal and stop the tray manager last, so any final
	// balloon notifications from shutting services have already drained.
	select {
	case <-a.trayStop:
	default:
		close(a.trayStop)
	}
	a.TrayManager.Stop()
	log.Println("Application shutdown complete")
}

// beforeClose is called when the user clicks the X button.
// Minimize to tray instead of quitting — unless user clicked "Quit" from tray.
func (a *App) beforeClose(ctx context.Context) (prevent bool) {
	if a.quitting {
		return false // Allow actual quit
	}
	runtime.WindowHide(a.ctx)
	return true // Minimize to tray
}

// startBackgroundServices starts per-session polling. If a previous session is
// still running (e.g. re-login without an explicit Logout), its goroutines are
// cancelled and drained BEFORE the new session is spawned. Without this, each
// re-login would leak another pollAttendance goroutine racing the last.
func (a *App) startBackgroundServices() {
	a.sessionMu.Lock()
	prevCancel := a.sessionCancel
	a.sessionCancel = nil
	a.sessionMu.Unlock()

	if prevCancel != nil {
		prevCancel()
	}
	// Drain previous goroutines before spawning new ones. Safe to call with
	// a zero WaitGroup (no-op when there are no pending goroutines).
	a.sessionWG.Wait()

	a.sessionMu.Lock()
	sctx, scancel := context.WithCancel(a.ctx)
	a.sessionCtx = sctx
	a.sessionCancel = scancel
	a.sessionMu.Unlock()

	a.sessionWG.Add(1)
	go func() {
		defer a.sessionWG.Done()
		defer func() {
			if r := recover(); r != nil {
				log.Printf("pollAttendance recovered from panic: %v", r)
			}
		}()
		a.pollAttendance(sctx)
	}()
}

// stopBackgroundServices cancels the session context and waits for all
// per-session goroutines to exit. Does not return until every goroutine has
// observed the cancellation — this is what guarantees the next Start cannot
// race a stale goroutine.
func (a *App) stopBackgroundServices() {
	a.sessionMu.Lock()
	cancel := a.sessionCancel
	a.sessionCancel = nil
	a.sessionMu.Unlock()

	if cancel != nil {
		cancel()
	}
	a.sessionWG.Wait()
}

// pollAttendance polls GET /attendance/me every 10 seconds to sync with web app.
func (a *App) pollAttendance(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	a.fetchAttendance()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.fetchAttendance()
		}
	}
}

// fetchAttendance fetches current attendance and emits updates to the frontend.
// Also starts/stops activity monitor based on timer state.
func (a *App) fetchAttendance() {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("fetchAttendance recovered from panic: %v", r)
		}
	}()
	attendance, err := a.APIClient.GetMyAttendance()
	if err != nil {
		a.networkErrorCount++
		log.Printf("Failed to fetch attendance (attempt %d): %v", a.networkErrorCount, err)
		if a.networkErrorCount >= 3 {
			runtime.EventsEmit(a.ctx, "network:error", "Connection lost. Retrying...")
		}
		return
	}
	if a.networkErrorCount > 0 {
		a.networkErrorCount = 0
		runtime.EventsEmit(a.ctx, "network:restored", nil)
	}

	a.State.SetAttendance(attendance)

	timerActive := attendance != nil && attendance.Status == "SIGNED_IN"

	if timerActive {
		a.TrayManager.SetTimerActive(true, attendance.CurrentTask)
		a.ActivityMonitor.Start(a.ctx)
	} else {
		a.TrayManager.SetTimerActive(false, nil)
		a.ActivityMonitor.Stop()
	}

	runtime.EventsEmit(a.ctx, "attendance:updated", attendance)
}

// Login handles user authentication. Called from frontend.
func (a *App) Login(email, password string) (*auth.LoginResult, error) {
	// Validate inputs
	email = strings.TrimSpace(email)
	if email == "" {
		return nil, fmt.Errorf("email or employee ID is required")
	}
	if len(email) > 256 {
		return nil, fmt.Errorf("email too long")
	}
	if password == "" {
		return nil, fmt.Errorf("password is required")
	}
	if len(password) > 256 {
		return nil, fmt.Errorf("password too long")
	}

	result, err := a.AuthService.Login(email, password)
	if err != nil {
		return nil, err
	}

	if result.RequiresNewPassword {
		return result, nil
	}

	a.State.SetAuthenticated(true)
	a.startBackgroundServices()
	return result, nil
}

// SetNewPassword completes the NEW_PASSWORD_REQUIRED challenge.
// The Cognito session token stays on the Go side — the frontend never sees it
// and never passes it back (see C-AUTH-2 / T-AUTH-2).
func (a *App) SetNewPassword(newPassword string) error {
	if len(newPassword) < 8 {
		return fmt.Errorf("password must be at least 8 characters")
	}
	if len(newPassword) > 256 {
		return fmt.Errorf("password too long")
	}
	if err := a.AuthService.CompleteNewPasswordChallenge(newPassword); err != nil {
		return err
	}

	a.State.SetAuthenticated(true)
	a.startBackgroundServices()
	return nil
}

// Logout clears tokens and stops background services.
func (a *App) Logout() error {
	a.stopBackgroundServices()
	a.ActivityMonitor.Stop()
	a.AuthService.Logout()
	a.State.SetAuthenticated(false)
	a.State.SetAttendance(nil)
	// NOTE: a.ctx is never replaced — it's the Wails context for the app's lifetime
	return nil
}

// SignIn starts a timer on a task. Called from frontend.
func (a *App) SignIn(data api.StartTimerData) (*api.Attendance, error) {
	// Validate description is present
	data.Description = strings.TrimSpace(data.Description)
	if data.Description == "" {
		return nil, fmt.Errorf("description is required")
	}
	if len(data.Description) > 500 {
		return nil, fmt.Errorf("description too long")
	}
	// Sanitize all string fields
	data.TaskID = strings.TrimSpace(data.TaskID)
	data.ProjectID = strings.TrimSpace(data.ProjectID)
	data.TaskTitle = strings.TrimSpace(data.TaskTitle)
	data.ProjectName = strings.TrimSpace(data.ProjectName)

	attendance, err := a.APIClient.SignIn(data)
	if err != nil {
		return nil, err
	}

	a.State.SetAttendance(attendance)
	a.TrayManager.SetTimerActive(true, attendance.CurrentTask)
	a.ActivityMonitor.Start(a.ctx) // Start monitoring when timer starts
	runtime.EventsEmit(a.ctx, "attendance:updated", attendance)
	return attendance, nil
}

// SignOut stops the current timer. Called from frontend.
func (a *App) SignOut() (*api.Attendance, error) {
	attendance, err := a.APIClient.SignOut()
	if err != nil {
		return nil, err
	}

	a.State.SetAttendance(attendance)
	a.TrayManager.SetTimerActive(false, nil)
	a.ActivityMonitor.Stop() // Stop monitoring when timer stops
	runtime.EventsEmit(a.ctx, "attendance:updated", attendance)
	return attendance, nil
}

// GetMyAttendance returns today's attendance. Called from frontend on mount.
func (a *App) GetMyAttendance() (*api.Attendance, error) {
	return a.APIClient.GetMyAttendance()
}

// GetMyTasks returns the current user's assigned tasks. Called from frontend.
func (a *App) GetMyTasks() ([]api.Task, error) {
	return a.APIClient.GetMyTasks()
}

// GetCurrentUser returns the authenticated user's profile. Called from frontend.
func (a *App) GetCurrentUser() (*api.User, error) {
	return a.APIClient.GetCurrentUser()
}

// ShowWindow restores the app window (called from tray).
func (a *App) ShowWindow() {
	log.Println("ShowWindow called")
	runtime.WindowShow(a.ctx)
	runtime.WindowUnminimise(a.ctx)
	// Briefly set always-on-top to bring to front, then release
	runtime.WindowSetAlwaysOnTop(a.ctx, true)
	go func() {
		time.Sleep(200 * time.Millisecond)
		runtime.WindowSetAlwaysOnTop(a.ctx, false)
	}()
}

// CheckForUpdate checks GitHub for a newer version. Called from frontend.
func (a *App) CheckForUpdate() (*updater.UpdateInfo, error) {
	return updater.CheckForUpdate()
}

// InstallUpdate downloads and installs the update. Called from frontend.
// On success, triggers a graceful Wails shutdown so background services and
// the activity monitor stop cleanly before the installer replaces the binary.
func (a *App) InstallUpdate(downloadURL, fileName string) error {
	if err := updater.DownloadAndInstall(&updater.UpdateInfo{
		Available:   true,
		DownloadURL: downloadURL,
		FileName:    fileName,
	}); err != nil {
		return err
	}
	runtime.Quit(a.ctx)
	return nil
}

// GetAppVersion returns the current app version.
func (a *App) GetAppVersion() string {
	return updater.CurrentVersion
}
