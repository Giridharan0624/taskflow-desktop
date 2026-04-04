package main

import (
	"context"
	"fmt"
	"log"
	"strings"
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
	ctx             context.Context // The EXACT Wails context — required for runtime calls
	stopChan        chan struct{}   // Signal to stop background goroutines
	State           *state.AppState
	AuthService     *auth.Service
	APIClient       *api.Client
	ActivityMonitor *monitor.ActivityMonitor
	TrayManager     *tray.Manager
}

// startup is called when the app starts. The context is saved for runtime calls.
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx // Store the EXACT Wails context — never wrap or replace it
	a.stopChan = make(chan struct{})

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

	// Start system tray (pure Win32 — no library conflict with Wails)
	go a.TrayManager.Start(a.stopChan)

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
	a.TrayManager.Stop()
	log.Println("Application shutdown complete")
}

// beforeClose is called when the user clicks the X button.
// Minimize to tray instead of quitting — timer + monitoring keep running.
func (a *App) beforeClose(ctx context.Context) (prevent bool) {
	runtime.WindowHide(a.ctx)
	return true // Prevent actual close — app stays in tray
}

// startBackgroundServices starts polling (activity monitor is started/stopped by timer state).
func (a *App) startBackgroundServices() {
	// Reset stop channel if it was previously closed
	select {
	case <-a.stopChan:
		a.stopChan = make(chan struct{})
	default:
	}

	go a.pollAttendance()
}

// stopBackgroundServices signals all goroutines to stop.
func (a *App) stopBackgroundServices() {
	select {
	case <-a.stopChan:
		// Already closed
	default:
		close(a.stopChan)
	}
}

// pollAttendance polls GET /attendance/me every 10 seconds to sync with web app.
func (a *App) pollAttendance() {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("pollAttendance recovered from panic: %v", r)
		}
	}()

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	a.fetchAttendance()

	for {
		select {
		case <-a.stopChan:
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
		log.Printf("Failed to fetch attendance: %v", err)
		return
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
func (a *App) SetNewPassword(session, newPassword string) error {
	if session == "" {
		return fmt.Errorf("invalid session")
	}
	if len(newPassword) < 8 {
		return fmt.Errorf("password must be at least 8 characters")
	}
	err := a.AuthService.CompleteNewPasswordChallenge(session, newPassword)
	if err != nil {
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
	runtime.WindowShow(a.ctx)
	runtime.WindowUnminimise(a.ctx)
	runtime.WindowSetAlwaysOnTop(a.ctx, false)
}

// CheckForUpdate checks GitHub for a newer version. Called from frontend.
func (a *App) CheckForUpdate() (*updater.UpdateInfo, error) {
	return updater.CheckForUpdate()
}

// InstallUpdate downloads and installs the update. Called from frontend.
func (a *App) InstallUpdate(downloadURL, fileName string) error {
	return updater.DownloadAndInstall(&updater.UpdateInfo{
		Available:   true,
		DownloadURL: downloadURL,
		FileName:    fileName,
	})
}

// GetAppVersion returns the current app version.
func (a *App) GetAppVersion() string {
	return updater.CurrentVersion
}
