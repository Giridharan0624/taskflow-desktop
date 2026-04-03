# Build Assets

Place the following files here before building:

- `appicon.png` — 1024x1024 app icon (used to generate all sizes)
- `windows/icon.ico` — Windows icon file (256x256 multi-resolution)
- `windows/installer/` — NSIS installer customization scripts (optional)

## Building

```bash
# Install Wails CLI (one-time)
go install github.com/wailsapp/wails/v2/cmd/wails@latest

# Development mode (hot reload)
wails dev

# Production build (creates .exe in build/bin/)
wails build

# Production build with NSIS installer
wails build -nsis
```

## Prerequisites

- Go 1.22+
- Node.js 18+
- Wails CLI v2
- WebView2 Runtime (pre-installed on Windows 10/11)
