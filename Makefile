PKG = taskflow-desktop/internal/config
UPDATER_PKG = taskflow-desktop/internal/updater
VERSION ?= 1.0.0

# Override these for production builds
API_URL ?= https://4saz9agwdi.execute-api.ap-south-1.amazonaws.com/staging
COGNITO_REGION ?= ap-south-1
COGNITO_POOL_ID ?= ap-south-1_NedaPlHsx
COGNITO_CLIENT_ID ?= 36i0ejo32b4c5u6un0g75h4bme
WEB_DASHBOARD_URL ?= https://taskflow-ns.vercel.app

LDFLAGS = -X '$(PKG).apiURL=$(API_URL)' \
          -X '$(PKG).cognitoRegion=$(COGNITO_REGION)' \
          -X '$(PKG).cognitoPoolID=$(COGNITO_POOL_ID)' \
          -X '$(PKG).cognitoClientID=$(COGNITO_CLIENT_ID)' \
          -X '$(PKG).webDashboardURL=$(WEB_DASHBOARD_URL)' \
          -X '$(UPDATER_PKG).CurrentVersion=$(VERSION)'

.PHONY: windows linux darwin all clean check

# Build for current platform (development)
dev:
	wails dev

# Windows (.exe)
windows:
	wails build -platform windows/amd64 -ldflags "$(LDFLAGS)"
	@echo "Built: build/bin/taskflow-desktop.exe"

# Linux (binary)
linux:
	wails build -platform linux/amd64 -ldflags "$(LDFLAGS)"
	@echo "Built: build/bin/taskflow-desktop"

# macOS (universal binary: Intel + Apple Silicon)
darwin:
	wails build -platform darwin/universal -ldflags "$(LDFLAGS)"
	@echo "Built: build/bin/taskflow-desktop.app"

# Build all platforms
all: windows linux darwin

# Verify Go compilation for all platforms (no Wails frontend)
check:
	GOOS=windows GOARCH=amd64 go build ./...
	GOOS=linux GOARCH=amd64 go build ./...
	@echo "Compilation check passed for windows and linux"
	@echo "Note: darwin requires CGo (macOS input_darwin.go) — build on macOS"

# Clean build artifacts
clean:
	rm -rf build/bin/*
