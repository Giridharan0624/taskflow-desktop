# TaskFlow Desktop — Cross-Platform Plan (Windows + Linux + macOS)

## Current State

The Windows desktop app is **in production with active users**. Development for Linux and macOS happens on a separate branch — main branch stays untouched until fully tested.

```
main branch              → Current Windows app (production, active users)
feature/cross-platform   → New cross-platform version (develop + test here)
                           ↓ merge only after all 3 platforms tested
                         main → Release new version to all platforms
```

Windows users only get the update when a new GitHub release is published.

---

## Cross-Platform Libraries

Instead of writing separate implementations per platform (21+ files), we use 3 libraries that work on all platforms:

| Feature | Library | Stars | Win | Linux | Mac | CGo |
|---------|---------|-------|-----|-------|-----|-----|
| Keyboard + Mouse | `go-vgo/robotgo` | 10.3k | Yes | Yes | Yes | Yes |
| Screenshots | `kbinani/screenshot` | 1.5k | Yes | Yes | Yes | Yes |
| System Tray | `fyne.io/systray` | 337 | Yes | Yes | Yes | Yes |

Only **2 features** need per-platform code (no library exists):

| Feature | Per-platform lines | Why |
|---------|-------------------|-----|
| Idle detection | ~30 lines each | No cross-platform library |
| Active window name | ~50 lines each | No library exists |

---

## File Changes

### Replaced by Cross-Platform Libraries

| Current File (Win32) | Lines | Replaced By |
|----------------------|-------|-------------|
| `internal/monitor/input.go` | 94 | `robotgo` (one file, all platforms) |
| `internal/monitor/screenshot.go` | 209 | `kbinani/screenshot` (one file, all platforms) |
| `internal/tray/tray.go` | 546 | `fyne.io/systray` (one file, all platforms) |

### Renamed with Build Tags

| Current | Renamed To |
|---------|-----------|
| `internal/monitor/window.go` | `internal/monitor/window_windows.go` |
| `internal/monitor/idle.go` | `internal/monitor/idle_windows.go` |
| `internal/auth/crypto.go` | `internal/auth/crypto_windows.go` |
| `internal/auth/keystore.go` | `internal/auth/keystore_windows.go` |

### New Files

```
Cross-platform (no build tags — works everywhere):
  internal/monitor/input.go            — robotgo keyboard + mouse
  internal/monitor/screenshot.go       — kbinani/screenshot
  internal/monitor/appnames.go         — friendlyAppName mapping (shared)
  internal/tray/types.go               — ActionHandler struct (shared)
  internal/tray/tray.go                — fyne.io/systray
  Makefile                             — Cross-platform build targets

Windows-specific (existing, add build tag):
  internal/monitor/window_windows.go   — Win32 GetForegroundWindow
  internal/monitor/idle_windows.go     — Win32 GetLastInputInfo
  internal/auth/crypto_windows.go      — DPAPI encryption
  internal/auth/keystore_windows.go    — Chunked keyring (Win Credential Manager limit)
  main_windows.go                      — APPDATA paths + windows.Options
  internal/updater/install_windows.go  — .exe asset finder

Linux-specific (new):
  internal/monitor/window_linux.go     — X11 _NET_ACTIVE_WINDOW + /proc/pid/exe
  internal/monitor/idle_linux.go       — X11 XScreenSaver extension
  internal/auth/crypto_linux.go        — No-op (secret-service encrypts)
  internal/auth/keystore_linux.go      — Direct keyring (no chunking)
  main_linux.go                        — XDG paths + linux.Options
  internal/updater/install_linux.go    — .AppImage asset finder
  build.sh                             — Linux build script
  build-appimage.sh                    — AppImage packaging
  build/linux/taskflow.desktop         — Freedesktop desktop entry
  build/linux/icon.png                 — App icon

macOS-specific (new):
  internal/monitor/window_darwin.go    — NSWorkspace frontmostApplication
  internal/monitor/idle_darwin.go      — IOKit HIDIdleTime
  internal/auth/crypto_darwin.go       — No-op (Keychain encrypts)
  internal/auth/keystore_darwin.go     — Direct keyring (no chunking)
  main_darwin.go                       — ~/Library paths + mac.Options
  internal/updater/install_darwin.go   — .dmg asset finder
  build-mac.sh                         — macOS build + .dmg + code signing
  build/darwin/icon.icns               — macOS icon

Unchanged (already cross-platform):
  app.go, internal/api/client.go, internal/config/config.go,
  internal/state/state.go, internal/auth/cognito.go,
  internal/monitor/activity.go, internal/updater/updater.go,
  frontend/* (all Preact/TypeScript)
```

---

## Implementation Phases

### Phase 1: Branch + Replace Libraries (Days 1-2)

1. Create `feature/cross-platform` branch
2. Replace `input.go` with robotgo-based implementation (all platforms)
3. Replace `screenshot.go` with kbinani/screenshot (all platforms)
4. Replace `tray.go` with fyne.io/systray (all platforms)
5. Extract shared code: `appnames.go`, `types.go`
6. Verify Windows still builds

### Phase 2: Platform-Specific Code (Days 3-4)

**Idle detection** (3 small files):
- Windows: `GetLastInputInfo` (existing, rename)
- Linux: X11 `XScreenSaverQueryInfo` via `BurntSushi/xgb`
- macOS: `IOKit HIDIdleTime` via CGo

**Active window** (3 small files):
- Windows: `GetForegroundWindow` (existing, rename)
- Linux: X11 `_NET_ACTIVE_WINDOW` → `/proc/[pid]/exe`
- macOS: `NSWorkspace.frontmostApplication` via CGo

**Auth/Crypto** (3 × 2 files):
- Windows: DPAPI + chunked keyring (existing, rename)
- Linux: No-op crypto + direct keyring
- macOS: No-op crypto + direct keyring

### Phase 3: Entry Points + Updater (Day 5)

**Main entry** (3 files):
- Windows: APPDATA log dir, windows.Options (existing, rename)
- Linux: `~/.local/share/TaskFlow/`, linux.Options
- macOS: `~/Library/Application Support/TaskFlow/`, mac.Options

**Updater install** (3 files):
- Windows: Find `.exe` asset, launch installer
- Linux: Find `.AppImage`, chmod +x, replace binary
- macOS: Find `.dmg`, open for user

### Phase 4: Build + Packaging (Days 6-7)

**Makefile**:
```makefile
windows:
    wails build -platform windows/amd64 -ldflags "$(LDFLAGS)"
linux:
    wails build -platform linux/amd64 -ldflags "$(LDFLAGS)"
mac:
    wails build -platform darwin/universal -ldflags "$(LDFLAGS)"
all: windows linux mac
```

**Linux packaging**: AppImage (universal, no install needed)
**macOS packaging**: .dmg + code signing + notarization ($99/yr Apple Developer)
**Windows packaging**: NSIS installer (existing)

### Phase 5: Testing (Days 8-9)

| Platform | Environment | Verify |
|----------|-------------|--------|
| Windows 10/11 | Dev machine | No regression |
| Ubuntu 22.04 (X11) | VM | All features work |
| Ubuntu 24.04 (Wayland) | VM | Graceful degradation |
| Fedora 38+ | VM | All features work |
| macOS 13+ (Intel) | Mac hardware | All features + permissions |
| macOS 14+ (Apple Silicon) | Mac hardware | Universal binary works |

---

## Build Dependencies

### Windows (existing)
```
Go 1.22+, Node.js 18+, Wails CLI, NSIS, GCC (MinGW)
```

### Linux (new)
```bash
# Ubuntu/Debian
sudo apt install gcc webkit2gtk-4.0-dev gtk3-dev \
  libayatana-appindicator3-dev libx11-dev libxtst-dev \
  libxkbcommon-dev xcb libxcb-xkb-dev

# Fedora
sudo dnf install gcc webkit2gtk4.0-devel gtk3-devel \
  libappindicator-gtk3-devel libX11-devel libXtst-devel
```

### macOS (new)
```
Xcode, create-dmg (brew install create-dmg)
Apple Developer certificate ($99/year for code signing)
```

---

## Distribution

| Platform | Format | Auto-Update Asset | Size |
|----------|--------|-------------------|------|
| Windows | NSIS installer (`.exe`) | `.exe` in GitHub release | ~5 MB |
| Linux | AppImage | `.AppImage` in GitHub release | ~15 MB |
| macOS | DMG (`.dmg`) | `.dmg` in GitHub release | ~10 MB |

GitHub release example:
```
v1.1.0
├── TaskFlowDesktop-Setup-1.1.0.exe          (Windows)
├── TaskFlow-1.1.0-x86_64.AppImage           (Linux)
└── TaskFlowDesktop-1.1.0-universal.dmg      (macOS)
```

---

## Wayland Limitations (Linux)

Wayland intentionally blocks global input monitoring for security. On pure Wayland:

| Feature | X11 | Wayland |
|---------|-----|---------|
| Keyboard/mouse counts | Full (robotgo) | Limited (robotgo uses XWayland) |
| Active window | Full | Returns "Unknown" |
| Screenshots | Full | Uses xdg-desktop-portal (user permission dialog) |
| Idle detection | Full (XScreenSaver) | Uses logind D-Bus (works) |

Most Wayland compositors run XWayland, so robotgo still works for input monitoring.

---

## macOS Permissions

| Permission | Prompt | Required For |
|-----------|--------|-------------|
| Accessibility | "TaskFlow wants to monitor input" | Keyboard/mouse tracking |
| Screen Recording | "TaskFlow wants to record screen" | Screenshots |
| Notifications | "TaskFlow wants to send notifications" | Screenshot warnings |

App must detect missing permissions and guide user to System Settings → Privacy & Security.

---

## Risks

| Risk | Impact | Mitigation |
|------|--------|-----------|
| robotgo CGo breaks Windows build | High | Test Windows first after adding |
| fyne.io/systray conflicts with Wails | Medium | Fallback: raw D-Bus / NSStatusBar |
| GNOME 40+ has no tray | Low | Document AppIndicator extension |
| macOS code signing costs $99/yr | Low | Distribute unsigned for testing |
| robotgo needs GCC everywhere | Medium | Document dependencies clearly |

---

## Timeline

| Phase | Days | What |
|-------|------|------|
| 1. Branch + cross-platform libraries | 2 | Replace Win32 with robotgo, kbinani, systray |
| 2. Platform-specific (idle, window, auth) | 2 | 9 small files (3 per platform) |
| 3. Entry points + updater | 1 | 6 files (2 per platform) |
| 4. Build scripts + packaging | 2 | Makefile, AppImage, .dmg, NSIS |
| 5. Testing all platforms | 2 | VMs + real hardware |
| **Total** | **9 days** | |

---

## Author

Developed by **Giridharan S** at **NEUROSTACK**

Copyright 2026 NEUROSTACK. All rights reserved.
