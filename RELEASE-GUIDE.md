# TaskFlow Desktop — Release Guide

## How to Release a New Version

### Step 1: Update Version Number

Update `$version` in both build scripts:

- `build.ps1` — line: `$version = "1.1.0"`
- `build-installer.ps1` — line: `$version = "1.1.0"`

Also update in:
- `wails.json` — `"productVersion": "1.1.0"`
- `frontend/package.json` — `"version": "1.1.0"`
- `build/windows/installer/project.nsi` — `!define PRODUCT_VERSION "1.1.0"` and `OutFile` name

### Step 2: Build the Installer

```powershell
cd D:\NEUROSTACK\PROJECTS\task-management\desktop
powershell -ExecutionPolicy Bypass -File build-installer.ps1
```

Output:
- `build\bin\taskflow-desktop.exe` — standalone exe
- `build\windows\installer\TaskFlowDesktop-Setup-1.1.0.exe` — installer

### Step 3: Commit and Tag

```bash
cd desktop
git add .
git commit -m "Release v1.1.0 — description of changes"
git tag v1.1.0
git push origin main
git push origin v1.1.0
```

### Step 4: Create GitHub Release

1. Go to https://github.com/Giridharan0624/taskflow-desktop/releases
2. Click **"Create a new release"**
3. Choose tag: `v1.1.0`
4. Title: `v1.1.0 — Brief description`
5. Description: List changes (bug fixes, new features)
6. Upload: `TaskFlowDesktop-Setup-1.1.0.exe`
7. Click **"Publish release"**

### Step 5: Done

All running desktop apps will check for updates on next launch. Users will see:

```
┌─────────────────────────────────────────┐
│  v1.1.0 available         [Update Now]  │
└─────────────────────────────────────────┘
```

Clicking "Update Now" downloads and launches the new installer automatically.

---

## How Auto-Update Works

```
App starts
  ↓ (5 second delay)
GET https://api.github.com/repos/Giridharan0624/taskflow-desktop/releases/latest
  ↓
Compare current version with latest tag
  ↓
If newer → show update banner in app
  ↓
User clicks "Update Now"
  ↓
Download .exe from GitHub release assets
  ↓
Launch installer → current app exits
  ↓
User runs new version
```

---

## Build Commands

| Command | What it does |
|---------|-------------|
| `powershell -File build.ps1` | Builds standalone `.exe` only |
| `powershell -File build-installer.ps1` | Builds `.exe` + NSIS installer |
| `wails dev` | Development mode with hot reload |

---

## Version Format

Use semantic versioning: `MAJOR.MINOR.PATCH`

- **MAJOR** — breaking changes (e.g., 2.0.0)
- **MINOR** — new features (e.g., 1.1.0)
- **PATCH** — bug fixes (e.g., 1.0.1)

---

## Deploying to Production

When deploying the backend for production:

```powershell
# Create Groq secret for production (one-time)
aws secretsmanager create-secret --name "taskflow/groq-api-key" --secret-string '{"api_key":"YOUR_KEY"}' --region ap-south-1

# Deploy production stack
cd D:\NEUROSTACK\PROJECTS\task-management\backend\cdk
cdk deploy --require-approval never
```

Update desktop `build.ps1` and `build-installer.ps1` with production config:
- Change `apiURL` to production API Gateway URL
- Change `cognitoPoolID` and `cognitoClientID` to production Cognito values

---

## Files Overview

```
desktop/
├── build.ps1               — Production exe build
├── build-installer.ps1     — Production installer build
├── dev.ps1                 — Dev mode launcher
├── config.json             — Local dev config (gitignored)
├── config.example.json     — Template for devs
├── clear-attendance.py     — DB cleanup utility
├── wails.json              — Wails app config
├── app.go                  — App logic (auth, timer, polling, updates)
├── main.go                 — Entry point (window config)
├── go.mod / go.sum         — Go dependencies
├── internal/
│   ├── auth/               — Cognito auth, DPAPI token encryption
│   ├── api/                — API client (HTTPS, TLS 1.3)
│   ├── config/             — Build-time config injection
│   ├── monitor/            — Activity tracking, screenshots, notifications
│   ├── state/              — Shared app state
│   ├── tray/               — System tray (Win32)
│   └── updater/            — Auto-update via GitHub releases
├── frontend/               — Preact + Vite UI
│   └── src/components/     — Timer, Login, TaskSelector, etc.
└── build/
    ├── bin/                — Built exe (gitignored)
    └── windows/
        ├── icon.ico        — App icon
        └── installer/      — NSIS script + privacy notice
```
