package updater

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	GitHubRepo   = "Giridharan0624/taskflow-desktop"
	CheckURL     = "https://api.github.com/repos/" + GitHubRepo + "/releases/latest"
	UserAgent    = "TaskFlow-Desktop-Updater/1.0"
)

// CurrentVersion is injected at build time via -ldflags.
var CurrentVersion = "1.0.0"

// Release represents a GitHub release.
type Release struct {
	TagName string  `json:"tag_name"`
	Name    string  `json:"name"`
	Body    string  `json:"body"` // Release notes
	Assets  []Asset `json:"assets"`
}

// Asset represents a downloadable file in a release.
type Asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int    `json:"size"`
}

// UpdateInfo is returned when an update is available.
type UpdateInfo struct {
	Available    bool   `json:"available"`
	Version      string `json:"version"`
	CurrentVer   string `json:"currentVersion"`
	DownloadURL  string `json:"downloadUrl"`
	ReleaseNotes string `json:"releaseNotes"`
	FileName     string `json:"fileName"`
	Size         int    `json:"size"`
}

// CheckForUpdate checks GitHub releases for a newer version.
func CheckForUpdate() (*UpdateInfo, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest("GET", CheckURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", UserAgent)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("network error: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 404 {
		// No releases yet
		return &UpdateInfo{Available: false, CurrentVer: CurrentVersion}, nil
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("GitHub API error: %d", resp.StatusCode)
	}

	var release Release
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, fmt.Errorf("failed to parse release: %w", err)
	}

	// Clean version tags (remove "v" prefix)
	latestVer := strings.TrimPrefix(release.TagName, "v")

	if !isNewer(latestVer, CurrentVersion) {
		return &UpdateInfo{Available: false, CurrentVer: CurrentVersion, Version: latestVer}, nil
	}

	// Find the platform-specific asset
	downloadURL, fileName, size := findPlatformAsset(release.Assets)

	if downloadURL == "" {
		return nil, fmt.Errorf("no compatible asset found in release %s", latestVer)
	}

	return &UpdateInfo{
		Available:    true,
		Version:      latestVer,
		CurrentVer:   CurrentVersion,
		DownloadURL:  downloadURL,
		ReleaseNotes: release.Body,
		FileName:     fileName,
		Size:         size,
	}, nil
}

// DownloadAndInstall downloads the update and launches the installer.
func DownloadAndInstall(info *UpdateInfo) error {
	if !info.Available || info.DownloadURL == "" {
		return fmt.Errorf("no update available")
	}

	log.Printf("Downloading update v%s from %s", info.Version, info.DownloadURL)

	// Download to temp directory
	tempDir := os.TempDir()
	destPath := filepath.Join(tempDir, info.FileName)

	client := &http.Client{Timeout: 5 * time.Minute}
	req, err := http.NewRequest("GET", info.DownloadURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", UserAgent)

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("download failed: HTTP %d", resp.StatusCode)
	}

	file, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}

	_, err = io.Copy(file, resp.Body)
	file.Close()
	if err != nil {
		return fmt.Errorf("download incomplete: %w", err)
	}

	log.Printf("Downloaded to %s, launching installer...", destPath)

	// Launch the platform-specific installer. The caller is responsible for
	// triggering a graceful app shutdown (runtime.Quit) so in-flight activity
	// heartbeats and background services are stopped cleanly before the
	// installer replaces the running binary.
	if err := installUpdate(destPath); err != nil {
		return fmt.Errorf("failed to install update: %w", err)
	}
	return nil
}

// isNewer reports whether version a is strictly newer than b.
// Components are compared numerically so "1.10.0" > "1.9.0" — a lexicographic
// compare would get that wrong because "10" < "9" as strings.
func isNewer(a, b string) bool {
	aParts := parseVersion(a)
	bParts := parseVersion(b)
	for i := 0; i < len(aParts) || i < len(bParts); i++ {
		av, bv := 0, 0
		if i < len(aParts) {
			av = aParts[i]
		}
		if i < len(bParts) {
			bv = bParts[i]
		}
		if av != bv {
			return av > bv
		}
	}
	return false
}

// parseVersion splits "1.2.3" into [1, 2, 3]. Pre-release suffixes like
// "-beta" or "+build" are stripped from each component. Returns whatever
// was parsed before the first invalid component.
func parseVersion(v string) []int {
	out := make([]int, 0, 3)
	for _, part := range strings.Split(v, ".") {
		if i := strings.IndexAny(part, "-+"); i >= 0 {
			part = part[:i]
		}
		n, err := strconv.Atoi(part)
		if err != nil {
			return out
		}
		out = append(out, n)
	}
	return out
}
