package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"taskflow-desktop/internal/api"
	"taskflow-desktop/internal/auth"
	"taskflow-desktop/internal/config"
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

	// quitting is set from the tray-OnQuit goroutine and read from Wails'
	// beforeClose thread. Without atomic.Bool, beforeClose can observe a
	// stale `false` and hide the window instead of allowing the quit —
	// leaving a ghost process with no tray icon in Task Manager.
	quitting atomic.Bool
	// networkErrorCount is read-modify-written from pollAttendance; if two
	// sessions were ever live at once (pre-fix), the ++ would race.
	networkErrorCount atomic.Int32
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
				a.quitting.Store(true)
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

	// Check for updates silently on startup. Deliberately cancellable —
	// a quick quit during the 5 s warm-up window (H-CORE-7) must not wake
	// up later and EventsEmit into a torn-down Wails context.
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("Startup update-check recovered from panic: %v", r)
			}
		}()
		select {
		case <-time.After(5 * time.Second):
		case <-a.ctx.Done():
			return
		}
		info, err := updater.CheckForUpdate()
		if err != nil {
			log.Printf("Update check failed: %v", err)
			return
		}
		if info.Available {
			log.Printf("Update available: v%s (current: v%s)", info.Version, info.CurrentVer)
			// Re-check cancellation just before crossing the IPC boundary;
			// a.ctx may have been torn down between the sleep and now.
			if a.ctx.Err() != nil {
				return
			}
			runtime.EventsEmit(a.ctx, "update:available", info)
		}
	}()
}

// shutdown is called when the app is closing.
func (a *App) shutdown(ctx context.Context) {
	a.stopBackgroundServices()
	a.ActivityMonitor.Stop()
	// Close the shared X11 connection (Linux) / no-op (Windows, macOS)
	// now that every goroutine that could be holding it has drained.
	// Safe on non-Linux because the build-tagged stub is a no-op.
	monitor.CloseX11()
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
// atomic.Bool.Load establishes a happens-before edge with the Store in the
// tray OnQuit goroutine, so we never observe a stale `false` here after the
// tray has committed to a full quit.
func (a *App) beforeClose(ctx context.Context) (prevent bool) {
	if a.quitting.Load() {
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

// currentSessionCtx returns the context scoped to the current login
// session, or a.ctx if no session is active. Callers must use this
// — NOT a.ctx — when starting per-session work (ActivityMonitor, any
// goroutine that should die on logout). Passing a.ctx here used to
// cause the monitor started during sign-in to survive logout,
// leaking goroutines across re-login cycles.
func (a *App) currentSessionCtx() context.Context {
	a.sessionMu.Lock()
	defer a.sessionMu.Unlock()
	if a.sessionCtx != nil {
		return a.sessionCtx
	}
	return a.ctx
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
		// Distinguish "token revoked / expired" from "network down". Without
		// this the UI previously got stuck on "Connection lost. Retrying…"
		// forever after a Cognito refresh-token revocation.
		if errors.Is(err, api.ErrUnauthorized) {
			log.Printf("Attendance poll got 401 — forcing re-auth")
			a.stopBackgroundServices()
			a.ActivityMonitor.Stop()
			if logoutErr := a.AuthService.Logout(); logoutErr != nil {
				// Keyring delete failed — log but proceed with the
				// re-auth flow; the UI must not get wedged on this.
				log.Printf("Logout during auth:expired returned: %v", logoutErr)
			}
			a.State.SetAuthenticated(false)
			a.State.SetAttendance(nil)
			runtime.EventsEmit(a.ctx, "auth:expired", nil)
			return
		}
		count := a.networkErrorCount.Add(1)
		log.Printf("Failed to fetch attendance (attempt %d): %v", count, err)
		if count >= 3 {
			runtime.EventsEmit(a.ctx, "network:error", "Connection lost. Retrying...")
		}
		return
	}
	if a.networkErrorCount.Swap(0) > 0 {
		runtime.EventsEmit(a.ctx, "network:restored", nil)
	}

	a.State.SetAttendance(attendance)

	timerActive := attendance != nil && attendance.Status == "SIGNED_IN"

	if timerActive {
		a.TrayManager.SetTimerActive(true, attendance.CurrentTask)
		// Scope the monitor to the current LOGIN session, not the Wails
		// app lifetime. Otherwise a monitor started during session A
		// inherits a.ctx's cancellation (never fires during normal
		// logout) and outlives the logout → re-login transition,
		// duplicating goroutines on each cycle.
		a.ActivityMonitor.Start(a.currentSessionCtx())
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
// Keyring delete errors from AuthService.Logout are logged and surfaced
// to the caller so a genuinely broken keystore doesn't silently pretend
// everything is fine (H-AUTH-3). UI state is still torn down regardless
// so the user can always get back to the login screen.
func (a *App) Logout() error {
	a.stopBackgroundServices()
	a.ActivityMonitor.Stop()
	authErr := a.AuthService.Logout()
	a.State.SetAuthenticated(false)
	a.State.SetAttendance(nil)
	// NOTE: a.ctx is never replaced — it's the Wails context for the app's lifetime
	if authErr != nil {
		log.Printf("Logout: keystore delete returned: %v", authErr)
		return fmt.Errorf("signed out, but stored credentials may remain: %w", authErr)
	}
	return nil
}

// errNotAuthenticated is returned from any post-auth binding called
// while the session is torn down (before login, after logout, after a
// 401-triggered auth:expired). The frontend should react to this as
// "the user needs to log in again" rather than treating it as a
// network failure. See H-CORE-5.
var errNotAuthenticated = fmt.Errorf("not authenticated")

// requireAuth is the gate every post-auth Wails binding runs through.
// Before this, a late-arriving call (e.g. a button click handled while
// the poll loop was mid-logout) would hit AuthService.GetIDToken, get
// an opaque "not authenticated" error, and leave the UI in a broken
// state. Now we return a clean sentinel before touching the API client.
func (a *App) requireAuth() error {
	if a.State == nil || !a.State.IsAuthenticated() {
		return errNotAuthenticated
	}
	return nil
}

// SignIn starts a timer on a task. Called from frontend.
func (a *App) SignIn(data api.StartTimerData) (*api.Attendance, error) {
	if err := a.requireAuth(); err != nil {
		return nil, err
	}
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
	// Scoped to the current login session, not the app lifetime. See
	// currentSessionCtx() comment.
	a.ActivityMonitor.Start(a.currentSessionCtx()) // Start monitoring when timer starts
	runtime.EventsEmit(a.ctx, "attendance:updated", attendance)
	return attendance, nil
}

// SignOut stops the current timer. Called from frontend.
func (a *App) SignOut() (*api.Attendance, error) {
	if err := a.requireAuth(); err != nil {
		return nil, err
	}
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
	if err := a.requireAuth(); err != nil {
		return nil, err
	}
	return a.APIClient.GetMyAttendance()
}

// GetMyTasks returns the current user's assigned tasks. Called from frontend.
func (a *App) GetMyTasks() ([]api.Task, error) {
	if err := a.requireAuth(); err != nil {
		return nil, err
	}
	return a.APIClient.GetMyTasks()
}

// GetCurrentUser returns the authenticated user's profile. Called from frontend.
func (a *App) GetCurrentUser() (*api.User, error) {
	if err := a.requireAuth(); err != nil {
		return nil, err
	}
	return a.APIClient.GetCurrentUser()
}

// ShowWindow restores the app window (called from tray).
func (a *App) ShowWindow() {
	log.Println("ShowWindow called")
	runtime.WindowShow(a.ctx)
	runtime.WindowUnminimise(a.ctx)
	// Briefly set always-on-top to bring to front, then release. If the
	// app is quitting during that 200ms window, observe the cancel and
	// skip the final always-on-top(false) — runtime calls on a
	// cancelled Wails context log errors.
	runtime.WindowSetAlwaysOnTop(a.ctx, true)
	go func() {
		select {
		case <-time.After(200 * time.Millisecond):
			runtime.WindowSetAlwaysOnTop(a.ctx, false)
		case <-a.ctx.Done():
			return
		}
	}()
}

// CheckForUpdate checks GitHub for a newer version. Called from frontend.
func (a *App) CheckForUpdate() (*updater.UpdateInfo, error) {
	return updater.CheckForUpdate()
}

// InstallUpdate downloads and installs the latest available update.
//
// Deliberately takes no arguments: the download URL, filename, and
// checksum URL all stay Go-side. The frontend used to pass downloadURL
// and fileName back across the IPC boundary, which meant a compromised
// or XSS-tainted renderer could steer the updater at an arbitrary host.
// Re-fetching via updater.CheckForUpdate is one extra GitHub API call
// per install — a non-issue in practice, and the URLs don't cross IPC.
//
// On success, joins all background goroutines (session poll + activity
// monitor) BEFORE triggering Wails shutdown. Without this, the running
// .exe can still hold file handles (log file, keychain) when NSIS tries
// to overwrite it, and the installer silently fails. See H-UPD-3.
func (a *App) InstallUpdate() error {
	info, err := updater.CheckForUpdate()
	if err != nil {
		return fmt.Errorf("update check: %w", err)
	}
	if info == nil || !info.Available {
		return fmt.Errorf("no update available")
	}

	// Download + SHA-256 verify + launch installer. If this fails we
	// haven't torn anything down yet, so the user can retry without
	// having lost their session.
	if err := updater.DownloadAndInstall(info); err != nil {
		return err
	}

	// Installer launched (ShellExecute is fire-and-forget). Drain our
	// own goroutines so the running .exe releases file handles before
	// Wails shutdown fires. runtime.Quit will call a.shutdown which is
	// idempotent — re-stopping these is safe.
	a.stopBackgroundServices()
	a.ActivityMonitor.Stop()

	runtime.Quit(a.ctx)
	return nil
}

// GetAppVersion returns the current app version.
func (a *App) GetAppVersion() string {
	return updater.CurrentVersion
}

// GetWebDashboardURL returns the configured web dashboard URL so the
// footer link opens the correct environment (staging vs prod) instead of
// being hard-coded in the frontend. See M-FE-3.
func (a *App) GetWebDashboardURL() string {
	return config.Get().WebDashboardURL
}

// SessionInfo tells the frontend what display-server limitations apply
// to the current session so it can surface an honest banner to the
// user. The platform-specific detection lives in session_*.go files.
type SessionInfo struct {
	// Platform: "windows", "darwin", "linux".
	Platform string `json:"platform"`
	// SessionType: "x11", "wayland", "native", or "unknown". Only
	// meaningful on Linux; other platforms report "native".
	SessionType string `json:"sessionType"`
	// CanTrackWindows: true if per-app activity breakdown works with
	// full fidelity. False on Wayland-without-XWayland, where the
	// compositor does not expose focus to non-privileged apps.
	CanTrackWindows bool `json:"canTrackWindows"`
	// LimitationMessage: user-facing explanation of any reduced
	// tracking capability, or "" if everything works. The frontend
	// shows this as a dismissible banner on first sign-in.
	LimitationMessage string `json:"limitationMessage"`
}

// GetSessionInfo reports display-server capabilities so the UI can
// warn Wayland users that per-app tracking is limited by the
// compositor's security model, not by our code.
func (a *App) GetSessionInfo() *SessionInfo {
	return detectSessionInfo()
}
