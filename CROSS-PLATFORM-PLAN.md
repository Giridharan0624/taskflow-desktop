# TaskFlow Desktop — macOS & Linux Plan

## Current State (Windows Only)

The desktop app uses several Windows-specific APIs:

| Feature | Windows API Used | Portable? |
|---------|-----------------|-----------|
| Activity tracking | `GetAsyncKeyState` (user32.dll) | No |
| Mouse tracking | `GetCursorPos` (user32.dll) | No |
| Window tracking | `GetForegroundWindow` (user32.dll) | No |
| Idle detection | `GetLastInputInfo` (user32.dll) | No |
| Screenshots | `BitBlt` (gdi32.dll) | No |
| Token encryption | DPAPI (`CryptProtectData`) | No |
| System tray | `Shell_NotifyIcon` (shell32.dll) | No |
| Tray menu | Win32 popup menus | No |
| Tray icon overlay | GDI bitmap manipulation | No |
| Installer | NSIS | No |

**14 files with Windows-specific code** need platform alternatives.

---

## Architecture Change

### Current (Windows Only)
```
internal/monitor/
  ├── input.go          ← Win32 GetAsyncKeyState
  ├── window.go         ← Win32 GetForegroundWindow
  ├── idle.go           ← Win32 GetLastInputInfo
  ├── screenshot.go     ← Win32 BitBlt + GDI
internal/auth/
  └── crypto.go         ← Win32 DPAPI
internal/tray/
  └── tray.go           ← Win32 Shell_NotifyIcon
```

### Proposed (Cross-Platform)
```
internal/monitor/
  ├── input_windows.go      ← Win32 GetAsyncKeyState
  ├── input_darwin.go       ← CGEventTap (macOS)
  ├── input_linux.go        ← /dev/input or X11
  ├── window_windows.go     ← Win32 GetForegroundWindow
  ├── window_darwin.go      ← NSWorkspace (macOS)
  ├── window_linux.go       ← X11 _NET_ACTIVE_WINDOW
  ├── idle_windows.go       ← Win32 GetLastInputInfo
  ├── idle_darwin.go        ← IOKit HIDIdleTime
  ├── idle_linux.go         ← X11 XScreenSaverQueryInfo
  ├── screenshot_windows.go ← Win32 BitBlt
  ├── screenshot_darwin.go  ← CGWindowListCreateImage
  ├── screenshot_linux.go   ← X11 XGetImage or PipeWire
  ├── activity.go           ← Shared (no changes)
  └── interfaces.go         ← Shared interfaces
internal/auth/
  ├── crypto_windows.go     ← DPAPI
  ├── crypto_darwin.go      ← macOS Keychain (Security.framework)
  └── crypto_linux.go       ← libsecret or GNOME Keyring
internal/tray/
  ├── tray_windows.go       ← Shell_NotifyIcon
  ├── tray_darwin.go        ← NSStatusBar
  └── tray_linux.go         ← libappindicator or systray
```

Go's **build tags** (`//go:build darwin`, `//go:build linux`) handle platform selection at compile time — no runtime checks needed.

---

## macOS Plan

### Requirements
- macOS 12+ (Monterey)
- Apple Silicon (arm64) + Intel (amd64) universal binary
- Wails v2 uses WebKit on macOS (built-in, no downloads needed)

### Platform-Specific Implementations

| Feature | macOS Implementation | Notes |
|---------|---------------------|-------|
| **Activity tracking** | `CGEventTap` via cgo | Requires Accessibility permission |
| **Mouse tracking** | `CGEventTap` (mouse events) | Same permission as keyboard |
| **Window tracking** | `NSWorkspace.shared.frontmostApplication` | No extra permissions |
| **Idle detection** | `IOKit HIDIdleTime` property | No permissions needed |
| **Screenshots** | `CGWindowListCreateImage` | Requires Screen Recording permission |
| **Token encryption** | macOS Keychain (`SecItemAdd`) | Built-in, encrypted by OS |
| **System tray** | `NSStatusBar` / `NSStatusItem` | Native menu bar icon |
| **Installer** | `.dmg` disk image | Standard macOS distribution |
| **Auto-start** | Login Items (SMAppService) | macOS 13+ API |
| **Notifications** | `NSUserNotification` / `UNUserNotification` | Standard macOS alerts |

### Permissions Required

| Permission | macOS Prompt | Why |
|-----------|-------------|-----|
| **Accessibility** | "TaskFlow wants to monitor input" | Keyboard/mouse tracking |
| **Screen Recording** | "TaskFlow wants to record screen" | Screenshots |
| **Notifications** | "TaskFlow wants to send notifications" | Screenshot warnings |

Users must grant these in **System Preferences → Privacy & Security**. The app should detect missing permissions and guide the user.

### Build & Distribution

```bash
# Build universal binary (arm64 + amd64)
wails build -platform darwin/universal

# Create .dmg installer
create-dmg \
  --volname "TaskFlow Desktop" \
  --window-size 600 400 \
  --app-drop-link 400 200 \
  "TaskFlowDesktop-1.0.0.dmg" \
  "build/bin/TaskFlow Desktop.app"
```

### Code Signing (Required for macOS)

macOS **requires** code signing for:
- Gatekeeper to allow the app
- Notarization for distribution outside App Store
- Accessibility/Screen Recording permissions

```bash
# Sign with Developer ID
codesign --deep --force --sign "Developer ID Application: NEUROSTACK" \
  "build/bin/TaskFlow Desktop.app"

# Notarize with Apple
xcrun notarytool submit TaskFlowDesktop-1.0.0.dmg \
  --apple-id "dev@neurostack.in" \
  --team-id "XXXXXXXXXX" \
  --wait
```

**Cost:** Apple Developer Program = $99/year

### Estimated Effort: 2-3 weeks

| Task | Days |
|------|------|
| Input monitoring (CGEventTap) | 3 |
| Window tracking (NSWorkspace) | 1 |
| Idle detection (IOKit) | 1 |
| Screenshots (CGWindowListCreateImage) | 2 |
| Keychain integration | 1 |
| System tray (NSStatusBar) | 2 |
| Permission flow (Accessibility, Screen Recording) | 2 |
| .dmg installer + code signing + notarization | 2 |
| Testing on Intel + Apple Silicon | 1 |

---

## Linux Plan

### Requirements
- Ubuntu 20.04+ / Fedora 35+ / Arch (current)
- X11 or Wayland display server
- Wails v2 uses `webkit2gtk` on Linux

### Platform-Specific Implementations

| Feature | Linux Implementation | Notes |
|---------|---------------------|-------|
| **Activity tracking** | `/dev/input` (evdev) or X11 `XQueryKeymap` | Needs root or input group |
| **Mouse tracking** | `/dev/input` or X11 `XQueryPointer` | Same as keyboard |
| **Window tracking** | X11 `_NET_ACTIVE_WINDOW` or `xdotool` | Wayland: `wlr-foreign-toplevel` |
| **Idle detection** | X11 `XScreenSaverQueryInfo` | Wayland: `ext-idle-notify-v1` |
| **Screenshots** | X11 `XGetImage` or PipeWire | Wayland: `xdg-desktop-portal` |
| **Token encryption** | libsecret (GNOME Keyring) or KWallet | Depends on desktop environment |
| **System tray** | libappindicator / StatusNotifierItem | Some DEs removed tray support |
| **Installer** | `.AppImage` (universal) or `.deb` / `.rpm` | AppImage needs no install |
| **Auto-start** | `~/.config/autostart/taskflow.desktop` | XDG autostart standard |
| **Notifications** | `notify-send` or D-Bus `org.freedesktop.Notifications` | Standard on all DEs |

### X11 vs Wayland Challenge

| Feature | X11 | Wayland |
|---------|-----|---------|
| Input monitoring | Easy (XQueryKeymap) | **Blocked** — by design |
| Window tracking | Easy (_NET_ACTIVE_WINDOW) | Limited (needs portal) |
| Screenshots | Easy (XGetImage) | Needs `xdg-desktop-portal` |
| Idle detection | Easy (XScreenSaver) | Needs `ext-idle-notify` |

**Wayland intentionally blocks input monitoring** for security. Options:
1. Require X11 (or XWayland) — simplest
2. Use Wayland portals — limited, needs user approval each time
3. Use evdev (`/dev/input`) — needs `input` group membership

### Build & Distribution

```bash
# Build
wails build -platform linux/amd64

# Create AppImage (universal, no install needed)
linuxdeploy --appdir AppDir \
  --executable build/bin/taskflow-desktop \
  --desktop-file taskflow.desktop \
  --icon-file icon.png \
  --output appimage

# Or create .deb package
dpkg-deb --build taskflow-desktop_1.0.0_amd64
```

### Dependencies

```bash
# Ubuntu/Debian
sudo apt install webkit2gtk-4.0-dev libappindicator3-dev libsecret-1-dev

# Fedora
sudo dnf install webkit2gtk4.0-devel libappindicator-gtk3-devel libsecret-devel
```

### Estimated Effort: 3-4 weeks

| Task | Days |
|------|------|
| Input monitoring (evdev / X11) | 4 |
| Wayland compatibility research + fallbacks | 3 |
| Window tracking (X11 + Wayland) | 2 |
| Idle detection (XScreenSaver + Wayland) | 1 |
| Screenshots (X11 + xdg-desktop-portal) | 3 |
| libsecret keyring integration | 1 |
| System tray (libappindicator) | 2 |
| AppImage / .deb packaging | 2 |
| Testing on Ubuntu, Fedora, Arch | 2 |

---

## Shared Code (No Changes Needed)

These files work on all platforms without modification:

| File | Why |
|------|-----|
| `app.go` | Pure Go, Wails runtime |
| `main.go` | Wails entry point |
| `internal/api/client.go` | HTTP client (net/http) |
| `internal/auth/cognito.go` | AWS SDK (pure Go) |
| `internal/auth/keystore.go` | Calls platform crypto, no OS deps |
| `internal/config/config.go` | Build-time injection |
| `internal/state/state.go` | In-memory state |
| `internal/updater/updater.go` | HTTP + JSON (pure Go) |
| `internal/monitor/activity.go` | Orchestrator (calls platform interfaces) |
| `frontend/*` | Preact UI (runs in WebView) |

**~60% of the codebase is already cross-platform.**

---

## Recommended Order

1. **macOS first** — larger market share, more consistent platform (no X11/Wayland split)
2. **Linux second** — smaller audience, more fragmentation (distros, display servers)

---

## CI/CD for Multi-Platform

```yaml
# GitHub Actions — build on all platforms
jobs:
  build:
    strategy:
      matrix:
        os: [windows-latest, macos-latest, ubuntu-latest]
    runs-on: ${{ matrix.os }}
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: '1.22' }
      - uses: actions/setup-node@v4
        with: { node-version: '18' }
      - run: go install github.com/wailsapp/wails/v2/cmd/wails@latest
      - run: wails build
      - uses: actions/upload-artifact@v4
        with:
          name: taskflow-desktop-${{ matrix.os }}
          path: build/bin/*
```

---

## Summary

| Platform | Effort | Key Challenge | Distribution |
|----------|--------|---------------|-------------|
| **Windows** | Done | N/A | NSIS installer (.exe) |
| **macOS** | 2-3 weeks | Code signing ($99/yr), permissions | .dmg + notarization |
| **Linux** | 3-4 weeks | Wayland input blocking | AppImage / .deb / .rpm |

Total estimated effort for full cross-platform: **5-7 weeks**.
