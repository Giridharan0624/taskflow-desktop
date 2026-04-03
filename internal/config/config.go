package config

import (
	"encoding/json"
	"fmt"
	"os"
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

var loaded *Config

// Get returns the app configuration. Values are baked in at build time.
// Falls back to config.json for dev mode (wails dev).
func Get() *Config {
	if loaded != nil {
		return loaded
	}

	loaded = &Config{
		APIURL:          apiURL,
		CognitoRegion:   cognitoRegion,
		CognitoPoolID:   cognitoPoolID,
		CognitoClientID: cognitoClientID,
		WebDashboardURL: webDashboardURL,
	}

	// If ldflags not injected (dev mode), try loading from config.json
	if loaded.APIURL == "" || loaded.CognitoClientID == "" {
		if err := loadFromFile(loaded); err != nil {
			panic("Config not available. For production: use build.ps1. For dev: create config.json from config.example.json.")
		}
	}

	return loaded
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
