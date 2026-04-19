package config

import (
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"os"
	"strings"
	"sync"
)

// These values are injected at build time via -ldflags for production builds.
// Example: go build -ldflags "-X taskflow-desktop/internal/config.apiURL=https://..."
// In dev mode (wails dev), they fall back to values from config.dev.json if present.
var (
	apiURL          = "" // Injected at build time
	cognitoRegion   = "" // Injected at build time
	cognitoPoolID   = "" // Injected at build time
	cognitoClientID = "" // Injected at build time
	webDashboardURL = "" // Injected at build time
)

// Config holds all environment-specific settings.
type Config struct {
	APIURL          string
	CognitoRegion   string
	CognitoPoolID   string
	CognitoClientID string
	WebDashboardURL string
}

var (
	cfgMu sync.Mutex
	cfg   *Config
)

// Get returns the app configuration. Values are baked in at build time
// (via -ldflags) and fall back to config.json for dev mode.
//
// Previous implementation used sync.Once, which has a nasty failure
// mode: if the init function panics (malformed config.json, missing
// fields), the Once is still marked done and `cfg` stays nil. Every
// subsequent caller then returns nil and the app crashes with an
// opaque nil-deref in a random goroutine far from the real cause. See
// V2-H2.
//
// The explicit-mutex pattern here allows a future retry path AND
// keeps the atomic-publication invariant (cfg is only set after the
// struct is fully populated AND validated; concurrent callers either
// block on the lock or observe the finished pointer — never a
// half-initialised one).
func Get() *Config {
	cfgMu.Lock()
	defer cfgMu.Unlock()
	if cfg != nil {
		return cfg
	}

	c := &Config{
		APIURL:          apiURL,
		CognitoRegion:   cognitoRegion,
		CognitoPoolID:   cognitoPoolID,
		CognitoClientID: cognitoClientID,
		WebDashboardURL: webDashboardURL,
	}
	// If ldflags weren't injected (dev mode), try loading from config.json.
	if c.APIURL == "" || c.CognitoClientID == "" {
		if err := loadFromFile(c); err != nil {
			panic("Config not available. For production: use build.ps1. For dev: create config.json from config.example.json.")
		}
	}
	if missing := missingFields(c); len(missing) > 0 {
		panic("Config incomplete — missing required fields: " + strings.Join(missing, ", "))
	}
	// Sanitise WebDashboardURL. It's optional (only the footer link
	// and tray menu use it), so silently clear it rather than fail
	// startup if it's malformed — that way the app still works, just
	// without a dashboard link. See V2-M1.
	if c.WebDashboardURL != "" && !isSafeDashboardURL(c.WebDashboardURL) {
		log.Printf("config: WebDashboardURL %q is not http(s) or contains userinfo — clearing", c.WebDashboardURL)
		c.WebDashboardURL = ""
	}
	cfg = c
	return cfg
}

// reset is for tests only — clears the cached config so the next Get
// call re-reads env / file / ldflags. Not exported in the public API.
func reset() {
	cfgMu.Lock()
	defer cfgMu.Unlock()
	cfg = nil
}

// isSafeDashboardURL accepts http(s) URLs with a host and no userinfo.
// Matches the contract in internal/tray/types.go:isSafeBrowserURL but
// duplicated here to avoid a config→tray import cycle.
func isSafeDashboardURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return false
	}
	if u.User != nil {
		return false
	}
	if u.Host == "" {
		return false
	}
	return true
}

// missingFields returns the names of required Config fields that are empty.
// WebDashboardURL is intentionally excluded — desktop still runs without it
// (only the dashboard link button is affected).
func missingFields(c *Config) []string {
	var missing []string
	if c.APIURL == "" {
		missing = append(missing, "APIURL")
	}
	if c.CognitoRegion == "" {
		missing = append(missing, "CognitoRegion")
	}
	if c.CognitoPoolID == "" {
		missing = append(missing, "CognitoPoolID")
	}
	if c.CognitoClientID == "" {
		missing = append(missing, "CognitoClientID")
	}
	return missing
}

// loadFromFile reads config.json for dev mode only.
func loadFromFile(cfg *Config) error {
	paths := []string{"config.json", "../config.json"}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var file struct {
			APIURL          string `json:"api_url"`
			CognitoRegion   string `json:"cognito_region"`
			CognitoPoolID   string `json:"cognito_user_pool_id"`
			CognitoClientID string `json:"cognito_client_id"`
			WebDashboardURL string `json:"web_dashboard_url"`
		}
		if err := json.Unmarshal(data, &file); err != nil {
			return err
		}
		cfg.APIURL = file.APIURL
		cfg.CognitoRegion = file.CognitoRegion
		cfg.CognitoPoolID = file.CognitoPoolID
		cfg.CognitoClientID = file.CognitoClientID
		cfg.WebDashboardURL = file.WebDashboardURL
		return nil
	}
	return fmt.Errorf("config.json not found")
}
