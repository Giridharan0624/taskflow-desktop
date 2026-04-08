# TaskFlow Desktop

**Cross-platform desktop companion app for TaskFlow — time tracking, activity monitoring, and screenshots.**

Built with Go (Wails v2) + Preact. Supports **Windows, Linux, and macOS**. Connects to the same backend as the web app.

---

## What It Does

| Feature | Details |
|---------|---------|
| **Timer** | Start/stop/switch tasks, meeting mode, syncs with web app (10s polling) |
| **Activity Monitoring** | Keyboard/mouse counts, active app tracking — only while timer is ON |
| **Screenshots** | Every 10 min with 5-second warning, skips locked screens, uploads to S3 |
| **System Tray** | Runs in background, red dot when recording, right-click menu |
| **AI Summary** | Groq LLaMA 3.3 analyzes daily activity on the web dashboard |
| **Auto-Update** | Checks GitHub releases on startup, one-click update |
| **Dark/Light Theme** | Matches the web app's theme, toggle in header |

---

## Tech Stack

```
Go 1.22              — Backend logic, platform APIs
Wails v2             — Desktop framework (WebView2 / WebKit)
Preact + Vite        — Frontend UI (~37KB JS)
TailwindCSS          — Styling
kbinani/screenshot   — Cross-platform screen capture (DXGI / X11 / CoreGraphics)
fyne.io/systray      — Cross-platform system tray
GitHub Actions       — CI/CD for all 3 platforms
```

### Platform Support

| Platform | Installer | Requirements |
|----------|-----------|-------------|
| **Windows 10/11** | NSIS installer (`.exe`) | None — WebView2 built-in |
| **Linux** (all distros) | AppImage | `webkit2gtk` (pre-installed on most DEs) |
| **macOS 13+** | DMG (`.dmg`) | Universal binary (Intel + Apple Silicon) |

---

## Security

- Tokens encrypted with **DPAPI** (Windows) / **Keychain** (macOS) / **secret-service** (Linux)
- All API calls use **HTTPS with TLS 1.3 minimum**
- Config injected at **build time** via ldflags (not stored on disk)
- Activity monitoring requires **user consent** (installer privacy notice)
- **No keystrokes recorded** — only press counts
- **No mouse coordinates** — only event counts
- Screenshots use **GPU-safe DXGI** (not BitBlt) — no conflicts with video calls
- Screenshots **skip locked screens** and show **5-second warning**
- Input validation on all user inputs

---

## Build

### Prerequisites

- Go 1.22+
- Node.js 18+
- GCC (MinGW on Windows, gcc on Linux, Xcode on macOS)
- Wails CLI (`go install github.com/wailsapp/wails/v2/cmd/wails@latest`)
- NSIS (Windows installer only)

### Development

```powershell
# Create config.json from template
cp config.example.json config.json
# Edit config.json with your Cognito/API values

# Windows
powershell -File dev.ps1

# Linux/macOS
wails dev
```

### Production Build (Local)

```powershell
# Windows: exe only
powershell -File build.ps1

# Windows: exe + NSIS installer
powershell -File build-installer.ps1
```

### Production Build (CI/CD — All Platforms)

```bash
# Push a version tag — GitHub Actions builds Windows + Linux + macOS automatically
git tag v1.1.0
git push origin v1.1.0
# Outputs: .exe installer, .AppImage, .dmg → GitHub Release + S3
```

See [CI-CD-SETUP.md](CI-CD-SETUP.md) for details.

Output:
```
build/bin/taskflow-desktop.exe                              (~15 MB)
build/windows/installer/TaskFlowDesktop-Setup-1.0.0.exe     (~5 MB)
```

### Config

Production config is injected at build time in `build.ps1` / `build-installer.ps1`:

```powershell
$ldflags = @(
    "-X '...config.apiURL=https://...amazonaws.com/prod'"
    "-X '...config.cognitoPoolID=ap-south-1_XXXXX'"
    "-X '...config.cognitoClientID=XXXXX'"
    "-X '...updater.CurrentVersion=1.0.0'"
)
```

For development, values are loaded from `config.json` (gitignored).

---

## Project Structure

```
desktop/
├── main.go                     # Wails entry point (window config)
├── app.go                      # App logic (auth, timer, polling, tray, updates)
├── go.mod / go.sum             # Go dependencies
├── wails.json                  # Wails config (app name, version)
├── config.json                 # Local dev config (gitignored)
├── config.example.json         # Template for developers
│
├── internal/
│   ├── auth/
│   │   ├── cognito.go          # Cognito login, token refresh, Employee ID resolution
│   │   ├── keystore.go         # Chunked keyring storage (handles Windows size limit)
│   │   └── crypto.go           # DPAPI encryption/decryption
│   ├── api/
│   │   └── client.go           # HTTP client (HTTPS, TLS 1.3, 30s timeout, snake↔camel)
│   ├── config/
│   │   └── config.go           # Build-time config injection with dev fallback
│   ├── monitor/
│   │   ├── activity.go         # Activity aggregator (5-min heartbeats, 10-min screenshots)
│   │   ├── input.go            # Win32 GetAsyncKeyState polling (keyboard/mouse counts)
│   │   ├── window.go           # Win32 GetForegroundWindow (active app name)
│   │   ├── idle.go             # Win32 GetLastInputInfo (idle detection)
│   │   └── screenshot.go       # Win32 BitBlt screen capture → JPEG
│   ├── tray/
│   │   └── tray.go             # Win32 Shell_NotifyIcon (system tray, menu, balloon)
│   ├── state/
│   │   └── state.go            # Thread-safe shared app state
│   └── updater/
│       └── updater.go          # GitHub releases auto-update checker
│
├── frontend/                   # Preact + Vite + TailwindCSS
│   ├── src/
│   │   ├── app.tsx             # Root component, Wails bindings, types
│   │   ├── components/
│   │   │   ├── TimerView.tsx   # Main timer UI (active/stopped states, sessions)
│   │   │   ├── TaskSelector.tsx# Source → Task → Start/Meeting selector
│   │   │   ├── LoginForm.tsx   # Email/Employee ID login + new password flow
│   │   │   ├── Timer.tsx       # Live HH:MM:SS display + formatDuration
│   │   │   ├── Logo.tsx        # TaskFlow logo component
│   │   │   ├── SessionList.tsx # Session list with resume buttons
│   │   │   └── ActivityBadge.tsx# Activity status indicator
│   │   ├── lib/
│   │   │   ├── useTheme.ts     # Light/dark theme toggle (persisted)
│   │   │   └── errors.ts       # User-friendly error message mapper
│   │   └── styles/
│   │       └── main.css        # Tailwind + CSS variables (light/dark)
│   ├── package.json
│   ├── vite.config.ts
│   └── tailwind.config.js
│
├── build/
│   ├── appicon.png             # App icon (512x512)
│   ├── windows/
│   │   ├── icon.ico            # Windows icon (multi-resolution)
│   │   └── installer/
│   │       ├── project.nsi     # NSIS installer script
│   │       └── privacy.txt     # Activity monitoring disclosure
│
├── build.ps1                   # Production exe build script
├── build-installer.ps1         # Production exe + installer build script
├── dev.ps1                     # Dev mode launcher
├── clear-attendance.py         # DB cleanup utility
└── RELEASE-GUIDE.md            # How to release new versions
```

---

## How It Works

### Timer Flow
```
User starts timer → POST /attendance/sign-in → Web sees it in 15s
                  → Activity monitor starts
                  → Every 5 min: heartbeat → POST /activity/heartbeat
                  → Every 10 min: screenshot → S3 → URL in heartbeat
User stops timer  → PUT /attendance/sign-out → Activity monitor stops
```

### Activity Data Collected (while timer is ON)

| Data | Method | Privacy |
|------|--------|---------|
| Keyboard press count | GetAsyncKeyState | No key values |
| Mouse event count | GetCursorPos + buttons | No coordinates |
| Active app name | GetForegroundWindow | No window titles |
| Active/idle seconds | GetLastInputInfo | Just duration |
| Screenshot | BitBlt + JPEG | 5-sec warning, skips locked |

### System Tray
```
Right-click tray icon:
┌─────────────────────────────┐
│ Working: task 1             │  ← status (grayed)
│─────────────────────────────│
│ Show Window                 │
│ Stop Timer                  │  ← only when active
│─────────────────────────────│
│ Open Dashboard              │
│─────────────────────────────│
│ Quit                        │
└─────────────────────────────┘
```

### Auto-Update
```
App starts → GET github.com/.../releases/latest
           → Compare version
           → Show banner: "v1.1.0 available [Update Now]"
           → Download .exe → Launch → Exit current
```

---

## Release Process

See [RELEASE-GUIDE.md](RELEASE-GUIDE.md) for detailed steps.

Quick summary:
1. Bump `$version` in `build.ps1` and `build-installer.ps1`
2. Build: `powershell -File build-installer.ps1`
3. Tag: `git tag v1.1.0 && git push origin v1.1.0`
4. Create GitHub release → upload installer `.exe`
5. All running apps will see the update on next launch

---

## Author

Developed by **Giridharan S** at **NEUROSTACK**

Copyright 2026 NEUROSTACK. All rights reserved.
