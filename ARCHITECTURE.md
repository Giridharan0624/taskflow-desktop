# TaskFlow Desktop — Architecture (Engineering Reference)

Engineering-level reference for the **Go + Wails v2 + Preact** desktop companion
app under `desktop/`. This is the shipping/reference implementation (a from-scratch
Rust/Tauri rewrite lives in `../desktop-rust/` and is **out of scope here**).

> This doc describes the code as it is. Where it contradicts
> [README.md](README.md), the code wins — see [Known doc drift](#known-doc-drift).
> For the timestamp-based timer model (shared with the web app) see
> [../docs/architecture/TIMER-ARCHITECTURE.md](../docs/architecture/TIMER-ARCHITECTURE.md).

Module path: `taskflow-desktop` (`go.mod`). Go 1.22, Wails v2, Preact frontend.

---

## 1. Overview & process model

A single Go process hosts a Wails WebView window. All UI is Preact/TS rendered in
the webview; all privileged work (auth, OS APIs, network, disk) is Go. The two
sides talk over Wails' IPC bridge.

- **`main.go`** — entry point: `ensureSingleInstance()` → `setupLogging()` →
  `NewApp()` → `wails.Run(...)`. Wires the Wails lifecycle callbacks
  (`startup`/`shutdown`/`beforeClose`) and binds the `App` struct's exported
  methods to the frontend. Sizes the window from the persisted `WindowSize`
  store (clamped, see `main.go` window constants), and installs a SIGINT/SIGTERM
  handler that auto-signs-out and quits gracefully with a watchdog.
- **[app.go](app.go)** — the `App` struct: the single object whose exported
  methods form the **entire Go↔JS API surface**, plus all orchestration
  (lifecycle, session goroutines, polling, timer, offline replay, updates).

The `App` struct holds every long-lived service:

| Field | Type | Package |
|---|---|---|
| `State` | `*state.AppState` | `internal/state` |
| `AuthService` | `*auth.Service` | `internal/auth` |
| `APIClient` | `*api.Client` | `internal/api` |
| `ActivityMonitor` | `*monitor.ActivityMonitor` | `internal/monitor` |
| `TrayManager` | `*tray.Manager` | `internal/tray` |
| `EventLog` | `*queue.EventLog` | `internal/queue` |
| `WindowSize` | `*queue.WindowSizeStore` | `internal/queue` |

**Two distinct lifetimes** run through the app — get this right or you leak
goroutines:

- **App lifetime** — `a.ctx` (the exact Wails context) and `a.trayStop`. Live
  from `startup` to `shutdown`. The tray and the startup update-check hang off
  this.
- **Login-session lifetime** — `a.sessionCtx` / `a.sessionCancel` / `a.sessionWG`.
  Created fresh on each login, cancelled on logout / 401 / re-login. Everything
  that should die when the user signs out (attendance poll, settings refresh,
  activity monitor) is scoped to this, obtained via `currentSessionCtx()` —
  **never `a.ctx`**.

---

## 2. Runtime lifecycle

`Wails callback` → `App` method:

- **`startup(ctx)`** — stores `a.ctx`; constructs services; tries
  `AuthService.TryRestoreSession()` (silent re-login from keyring) and, on
  success, `startBackgroundServices()`; starts the tray on `trayStop`; and, after
  a 5 s cancellable warm-up, fires a silent `updater.CheckForUpdate()` that emits
  `update:available` if newer.
- **`beforeClose(ctx)`** — user clicked the window **X**. Returns `true`
  (prevent close) and hides to tray, *unless* `a.quitting` (atomic) is set, in
  which case it allows the real quit. The atomic establishes a happens-before
  edge with the tray Quit goroutine so we never read a stale `false` and strand a
  ghost process.
- **`shutdown(ctx)`** — the catch-all for **every** termination path Wails sees
  (tray Quit, OS shutdown, SIGTERM). Calls `autoSignOutIfRunning(3s)`, drains
  session goroutines, stops the monitor, `monitor.CloseX11()`, closes `trayStop`,
  stops the tray, flushes the log file last.

Termination coordination:

- **`autoSignOutIfRunning(timeout)`** — POSTs a sign-out iff a timer is active,
  bounded by `timeout` so a flaky network can't wedge OS shutdown. Guarded by
  `autoSignOutOnce sync.Once` so the tray-Quit path (5 s budget) and the
  `shutdown` catch-all (3 s budget) and SIGTERM don't double-POST — first caller
  wins.

---

## 3. Session & concurrency model

The trickiest part of the app; most of the defensive code in [app.go](app.go)
exists to make re-login and logout race-free.

- **`startBackgroundServices()`** — cancels & `sessionWG.Wait()`s any prior
  session, then creates a new `sessionCtx` and spawns `pollAttendance` and
  `refreshOrgSettings` under `sessionWG`. Draining before spawning is what stops
  each re-login leaking another poll goroutine.
- **`currentSessionCtx()`** — returns `sessionCtx` (or `a.ctx` if none). The
  activity monitor is started with this so it dies on logout instead of
  surviving into the next session.
- **`stopBackgroundServices()`** — cancels `sessionCtx`, waits for drain;
  idempotent (second call finds `sessionCancel == nil` and returns).
- **`pollAttendance(ctx)`** — 10 s ticker calling `fetchAttendance()`; the
  bidirectional sync point with the web app.
- **`fetchAttendance()`** — `GET /attendance/me`; on `ErrUnauthorized` tears the
  session down and emits `auth:expired`; on transient failure counts up and emits
  `network:error` at ≥3; on recovery emits `network:restored` and kicks
  `drainEventLog`. Drives `ActivityMonitor.Start/Stop` to match timer state.

---

## 4. Package reference (`internal/`)

Every package follows Go's per-OS build-tag idiom: a shared file plus
`*_windows.go` / `*_linux.go` / `*_darwin.go` variants gated by `//go:build`
(the filename suffix is itself an implicit constraint). See
[Platform matrix](#8-platform-matrix).

### `auth` — Cognito auth + token persistence
`Service` ([internal/auth/cognito.go](internal/auth/cognito.go)) wraps the AWS
SDK v2 Cognito client with anonymous credentials.

- **Login** uses the **`USER_PASSWORD_AUTH`** flow (`InitiateAuth`) — password
  over TLS, **not SRP** (see [drift](#known-doc-drift)). Employee-ID identifiers
  (`NS-…` / `EMP-…`) are resolved to an email via a hardened unauth client
  (`GET /resolve-employee`) before auth.
- **`NEW_PASSWORD_REQUIRED`** challenge: the Cognito `Session` token is kept
  **Go-side** on `Service` and never crosses IPC; `CompleteNewPasswordChallenge`
  finishes it.
- **Tokens** stored in the OS keyring (`zalando/go-keyring`, service
  `taskflow-desktop`). On Windows they are **DPAPI-encrypted** (`crypto_windows.go`,
  current-user scope) then base64'd, then **chunked** at 2000 B to fit Credential
  Manager's limit (`keystore_windows.go`, crash-safe `saveChunked`). macOS =
  Keychain, Linux = secret-service.
- **Refresh** (`GetIDToken`): refreshes within 5 min of expiry; concurrent 401
  storms coalesced by `singleflight` on a refresh-token fingerprint
  (`doRefreshCall`). `verifyIDTokenClaims` is a cheap local `exp`/`iss` check, not
  a signature verify (server does that). AWS errors pass through `redactAWSError`.

### `api` — HTTP client
`Client` ([internal/api/client.go](internal/api/client.go)) over `go-resty`.
`NewClient` **panics if the base URL isn't HTTPS**. TLS **floor 1.3** on a shared
transport; HTTPS-only redirect policy. Two clients: main (30 s, 1 transport
retry) and uploads (3 min, S3 PUT). `request()` injects
`Authorization: Bearer <idToken>` per request. All bodies pass through
`snakeToCamel` (which also rejects non-JSON WAF pages). Endpoints:
`GET /orgs/current`, `GET /attendance/me`, `POST /attendance/sign-in`,
`PUT /attendance/sign-out`, `GET /users/me/tasks`, `GET /users/me`,
`POST /activity/heartbeat`, `GET /uploads/presign` + S3 PUT. Feature gates:
`ScreenshotsEnabled()` (fail-**closed**), `ActivityMonitoringEnabled()`
(fail-**open**). Sentinels in `errors.go`: `ErrUnauthorized`, `ErrNotAuthenticated`.

### `monitor` — activity engine
`ActivityMonitor` ([internal/monitor/activity.go](internal/monitor/activity.go))
runs 4 goroutines on `Start(ctx)`: `trackActivity`, `captureScreenshots`,
`sendHeartbeats`, `drainWorker`. Only runs while the timer is ON; each tick
re-checks `IsTimerActive()`. `Stop` cancels, waits, flushes the partial bucket
(so ≤5 min of activity isn't lost), resets the input tracker.

- **Counters** (`InputTracker`, per-OS): keyboard/mouse **press counts only** — no
  keystrokes, no coordinates. Windows polls `GetAsyncKeyState` + `GetCursorPos`
  deltas (no hooks/CGo); Linux `XQueryPointer` + MIT-SCREEN-SAVER; macOS CGo
  `CGEventSourceCounterForEventType` (needs Accessibility).
- **Idle** (`IdleDetector`): Windows `GetLastInputInfo`; Linux logind /
  screensaver (`idle_logind_linux.go`). Surfaced via `App.GetIdleSeconds` for the
  frontend idle prompt.
- **Active window** (`WindowTracker`): Windows `GetForegroundWindow`→PID→exe,
  mapped to a friendly name by `appnames.go`. Titles are **not** recorded.
- **Screenshots** (`ScreenshotCapture`): uses **`kbinani/screenshot`** (DXGI /
  X11 / CoreGraphics), JPEG q85, random 9–10 min cadence (`nextScreenshotDelay`,
  defeats jigglers), `IsScreenLocked()` skip. Failed uploads snapshot the paired
  activity bucket so the recovered S3 URL is re-linked to the exact counts.
- `x11_linux.go` / `x11_other.go` hold the shared X11 connection + `CloseX11()`.

### `queue` — durable offline queues
File-per-entry queues under `<appdata>/TaskFlow/queue/<name>` plus a `cache/`
dir. Atomic writes (temp + fsync + rename), lexicographically-sortable filenames,
FIFO `Drain` that stops on first persistent failure, per-queue size caps that
evict oldest. Members: `HeartbeatQueue` (server-dedup by timestamp, ~3000 cap),
`ScreenshotQueue` (JPEG + paired bucket), `EventLog` (timer SignIn/SignOut/
TaskSwitched, 500 cap), `TasksCache`, `WindowSizeStore`. `ClearAll()` backs the
settings "Clear local cache". Paths per-OS in `paths_*.go` (`appDataRoot`).

### `tray` — system tray
`Manager` ([internal/tray/types.go](internal/tray/types.go) + per-OS). **Windows**
(`tray_windows.go`) is a full native tray: hidden message-loop window,
`Shell_NotifyIconW`, right-click menu (Status / Show Window / Stop Timer / Open
Dashboard / Quit), GDI recording-dot overlay, balloon toasts; cross-thread calls
marshalled via `WM_APP_*` messages. **Linux/macOS trays are notification-only**
(menu actions duplicated as in-app React buttons) — Linux D-Bus
`org.freedesktop.Notifications`, macOS `osascript`. Browser/dashboard opens go
through `isSafeBrowserURL`.

### `updater` — GitHub-release auto-update
[internal/updater/updater.go](internal/updater/updater.go): `CheckForUpdate`
(GitHub releases API, `findPlatformAsset`, semver compare `isNewer`),
`DownloadAndInstall` (download → **SHA-256 verify** `verifyChecksum` → optional
**Ed25519 signature verify** `signature.go` + `release_pubkey.go` → per-OS
`installUpdate`). `BeginInstall`/`EndInstall` guard against double-installs.
`detectInstallOrigin` returns `ErrPackageManaged` for apt/rpm/snap installs so the
UI tells the user to use their package manager instead. `CurrentVersion == "dev"`
short-circuits update checks.

### `state` — shared state
`AppState` ([internal/state/state.go](internal/state/state.go)): RWMutex-guarded
`authenticated` / `attendance` / `idleSeconds` with **deep-copy getters**. Holds
the `Attendance` / `AttendanceSession` / `CurrentTask` DTOs (re-exported as
aliases from `api`). The timer "state machine" is just
`attendance.Status ∈ {SIGNED_IN, SIGNED_OUT}` — there is no separate timer type.

### `system` — auto-start at login
`Autostart` interface + per-OS impls: Windows `HKCU\...\Run` value, Linux
`~/.config/autostart/*.desktop`, macOS LaunchAgent plist. Bound via
`App.SetAutoStart` / `GetAutoStart` (the settings drawer hydrates from real OS
state).

### `config` — build-time config
[internal/config/config.go](internal/config/config.go): five package vars
(`apiURL`, `cognitoRegion`, `cognitoPoolID`, `cognitoClientID`, `webDashboardURL`)
injected via `-ldflags -X`. `Get()` uses an explicit mutex (not `sync.Once`, to
avoid the panic-marks-done trap), falls back to `config.json` in dev, validates
required fields (panics if missing), and sanitizes `WebDashboardURL`.

### `security` — trust-boundary helper
[internal/security/url.go](internal/security/url.go): `ValidateHTTPSURL(raw,
allowedHosts)` — HTTPS-only, no userinfo, host allowlist. Used before any
outbound call to a URL that originated from data (S3 presign, dashboard link).

---

## 5. Key data flows

**Timer start/stop** (see also
[../docs/architecture/TIMER-ARCHITECTURE.md](../docs/architecture/TIMER-ARCHITECTURE.md)):
`App.SignIn` validates + records an offline `EventSignIn` **before** the network
call → `POST /attendance/sign-in` → sets state, activates tray,
`ActivityMonitor.Start(currentSessionCtx())`. `App.SignOut` mirrors it with
`EventSignOut` + `ActivityMonitor.Stop()`. The 10 s poll reconciles with web.

**Activity & screenshots** (while timer ON): counters aggregate into a 5-minute
bucket → `POST /activity/heartbeat` (persisted to `HeartbeatQueue` first, then
drained). A screenshot fires once per random 9–10 min window, gated by tenant
flag `features.screenshots` — kept fresh by `refreshOrgSettings` (10 min cache,
fail-closed) so an OWNER toggling it in the web app takes effect within a tick.

**Offline resilience**: writes are captured to durable queues first. On
`network:restored`, `drainEventLog` replays timer events (idempotent — server 4xx
replays are dropped, only genuine network errors keep the entry queued) and
`drainWorker` flushes heartbeats + screenshots every 30 s.

---

## 6. Frontend & IPC

Preact + Vite + Tailwind in `frontend/` (only runtime dep is `preact`, ~37 KB
JS). Wails auto-generates bindings in `frontend/wailsjs/`. `src/app.tsx`
subscribes to backend events; components include `TimerView`, `TaskSelector`,
`LoginForm`, `SessionList`, `SettingsDrawer`, `IdlePrompt`, plus a `ui/` primitive
kit.

**Go→JS events** emitted via `runtime.EventsEmit`: `attendance:updated`,
`auth:expired`, `network:error`, `network:restored`, `update:available`,
`update:package-managed`.

**JS→Go** = exported `App` methods (`Login`, `SetNewPassword`, `SignIn`,
`SignOut`, `Logout`, `GetMyAttendance`, `GetMyTasks`, `GetCurrentUser`,
`ShowWindow`, `CheckForUpdate`, `InstallUpdate`, `GetAppVersion`,
`GetWebDashboardURL`, `SetAutoStart`, `GetAutoStart`, `ClearLocalCache`,
`ShowTrayNotification`, `SaveWindowSize`, `GetIdleSeconds`, `GetSessionInfo`).

**IPC security invariants** — secrets deliberately never cross the boundary:
- Every post-auth binding runs through `requireAuth()`, returning a clean
  `errNotAuthenticated` sentinel rather than touching the API client.
- The Cognito `NEW_PASSWORD_REQUIRED` session token stays Go-side.
- `InstallUpdate()` takes **no arguments** — the download URL, filename and
  checksum URL are re-fetched Go-side so a tainted renderer can't steer the
  updater at an arbitrary host.

---

## 7. Config & build

Config is baked at build time; nothing sensitive lives on disk in prod (dev uses
a gitignored `config.json`). Two environments only:

| Env | Stack | API / Cognito |
|---|---|---|
| **Staging** | `taskflow-v2` | `mcx0iyvisf…/prod` · pool `ap-south-1_yWxQYrYXp` · client `6eaa6ej7…` |
| **Production** | `taskflow` | `qhh92ze0rc…/prod` · pool `ap-south-1_KvHp1RVEE` · client `7dakaniqm…` |

Scripts (all set `CGO_ENABLED=1`, `CC=gcc`, MinGW on PATH):

| Script | Purpose |
|---|---|
| [dev.ps1](dev.ps1) | copies `config.<env>.json`→`config.json`, `wails dev` (`-Env staging\|prod`) |
| [build.ps1](build.ps1) | prod exe only (ldflags baked) |
| [build-installer.ps1](build-installer.ps1) | prod exe + NSIS installer |
| [build-installer-staging.ps1](build-installer-staging.ps1) | staging (`taskflow-v2`) exe + installer |
| [build-installer-company.ps1](build-installer-company.ps1) | prod (`taskflow`) exe + installer |
| [Makefile](Makefile) | `windows`/`linux`/`darwin`/`all`/`check`(cross-compile smoke)/`clean` |

`wails.json` sets `nsisType: custom`; the template is
`build/windows/installer/project.nsi`. Output:
`build/bin/taskflow-desktop.exe` (~15 MB) and
`build/windows/installer/TaskFlowDesktop-Setup-1.0.0.exe` (~5 MB). CI builds all
three platforms on a version tag (see [README.md](README.md) § Release Process).

---

## 8. Platform matrix

| Concern | Windows | Linux | macOS |
|---|---|---|---|
| Single-instance | named mutex (`main_windows.go`) | `flock` on `app.lock` (`main_linux.go`) | `flock` (`main_darwin.go`) |
| Input counts | `GetAsyncKeyState` + `GetCursorPos` | `XQueryPointer` + MIT-SCREEN-SAVER | CGo `CGEventSourceCounter…` |
| Idle | `GetLastInputInfo` | logind / screensaver | native |
| Active window | `GetForegroundWindow`→exe | X11 | native |
| Screenshot | DXGI (`kbinani/screenshot`) | X11 | CoreGraphics |
| Token crypto | DPAPI + chunked keyring | secret-service | Keychain |
| Auto-start | `HKCU\...\Run` | `.desktop` autostart | LaunchAgent plist |
| Tray | full native menu + balloon | notifications only (D-Bus) | notifications only (osascript) |
| Update install | replace exe + relaunch | `installUpdate` (pkg-managed aware) | `installUpdate` |

`session_windows.go` / `session_linux.go` / `session_darwin.go` back
`App.GetSessionInfo()`, reporting display-server limitations (e.g.
Wayland-without-XWayland can't expose focus, so per-app tracking degrades — the
UI shows an honest banner rather than silently under-reporting).

---

## Known doc drift

[README.md](README.md) is feature-oriented and has drifted from the code in two
places worth knowing:

1. **Auth**: README says Cognito **SRP**; the code uses **`USER_PASSWORD_AUTH`**
   (`internal/auth/cognito.go`) — password over TLS, not SRP.
2. **Screenshots**: README's structure section says Win32 **BitBlt**; the code
   uses **`kbinani/screenshot`** (DXGI Desktop Duplication) — deliberately, to
   avoid GPU conflicts with video calls (`internal/monitor/screenshot.go`).

The README's `internal/` layout is also pre-growth (single `input.go`/`window.go`
etc.); the real tree is the per-package, per-OS split documented above.

---

## See also

- [README.md](README.md) — features, privacy posture, install/build quickstart
- [../docs/architecture/TIMER-ARCHITECTURE.md](../docs/architecture/TIMER-ARCHITECTURE.md) — timestamp-based timer model
- [../CLAUDE.md](../CLAUDE.md) — repo-wide conventions, deploy landscape
- `../desktop-rust/` — the Rust/Tauri rewrite (separate implementation)
