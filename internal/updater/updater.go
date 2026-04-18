package updater

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"taskflow-desktop/internal/security"
)

const (
	GitHubRepo = "Giridharan0624/taskflow-desktop"
	CheckURL   = "https://api.github.com/repos/" + GitHubRepo + "/releases/latest"
	UserAgent  = "TaskFlow-Desktop-Updater/1.0"

	// ChecksumAssetName is the expected name of the release asset that
	// contains SHA-256 hashes for every installer binary in the release.
	// The release workflow (see .github/workflows/release.yml) must publish
	// this file or updates will be refused.
	ChecksumAssetName = "SHA256SUMS"
)

// allowedUpdateHosts are the only hostnames the updater will ever contact
// for downloading binaries or checksums. Everything else (including any
// redirect target) is rejected. This neutralizes a compromised-GitHub-API
// or MITM attacker who might otherwise supply a hostile download URL.
var allowedUpdateHosts = []string{
	"github.com",
	"objects.githubusercontent.com",
	"github-releases.githubusercontent.com",
}

// CurrentVersion is injected at build time via -ldflags. The default
// "dev" sentinel causes CheckForUpdate to early-return: without it,
// wails dev builds would see the live GitHub release as "an update"
// and spam update:available events on startup. See H-UPD-2.
var CurrentVersion = "dev"

// installInProgress guards InstallUpdate against double-invocation.
// A user double-clicking "Update Now" used to launch two parallel
// downloads and two UAC prompts; the second install could truncate
// the installer the first was still writing to. See H-UPD-1.
var installInProgress atomic.Bool

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
	// ChecksumURL is the URL of the SHA256SUMS asset for this release.
	// Populated by CheckForUpdate; not sent to the frontend.
	ChecksumURL string `json:"-"`
}

// CheckForUpdate checks GitHub releases for a newer version.
// Dev builds (CurrentVersion == "dev") short-circuit here — we never
// want a wails-dev session to chase "updates" against the live
// GitHub release, and ldflags inject the real version in prod builds.
// See H-UPD-2.
func CheckForUpdate() (*UpdateInfo, error) {
	if CurrentVersion == "dev" {
		return &UpdateInfo{Available: false, CurrentVer: "dev"}, nil
	}

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

	// Skip pre-release and build-metadata tags (e.g. "1.6.0-beta", "1.6.0+rc1").
	// parseVersion strips these suffixes when comparing, so without this
	// guard beta tags would be silently treated as equal to the stable
	// release and auto-delivered to stable users. See M-UPD-1.
	if strings.ContainsAny(latestVer, "-+") {
		log.Printf("updater: skipping pre-release tag %q", release.TagName)
		return &UpdateInfo{Available: false, CurrentVer: CurrentVersion, Version: latestVer}, nil
	}

	if !isNewer(latestVer, CurrentVersion) {
		// Log remote-older-than-local as a distinct event so a rollback
		// attack (attacker republishes an older release) is at least
		// visible in the log file. See M-UPD-2.
		if isNewer(CurrentVersion, latestVer) {
			log.Printf("updater: remote release %q is older than current %q — ignoring", latestVer, CurrentVersion)
		}
		return &UpdateInfo{Available: false, CurrentVer: CurrentVersion, Version: latestVer}, nil
	}

	// Find the platform-specific asset
	downloadURL, fileName, size := findPlatformAsset(release.Assets)

	if downloadURL == "" {
		return nil, fmt.Errorf("no compatible asset found in release %s", latestVer)
	}

	// Find the SHA256SUMS asset in the same release. If it is missing we do
	// not refuse the check (the frontend still shows "update available"),
	// but DownloadAndInstall will later refuse to install an unverified
	// binary.
	checksumURL := ""
	for _, asset := range release.Assets {
		if asset.Name == ChecksumAssetName {
			checksumURL = asset.BrowserDownloadURL
			break
		}
	}

	return &UpdateInfo{
		Available:    true,
		Version:      latestVer,
		CurrentVer:   CurrentVersion,
		DownloadURL:  downloadURL,
		ReleaseNotes: release.Body,
		FileName:     fileName,
		Size:         size,
		ChecksumURL:  checksumURL,
	}, nil
}

// DownloadAndInstall downloads the update, verifies its SHA-256 hash against
// the release's SHA256SUMS file, and launches the platform installer.
//
// Security invariants (see C-UPD-1..4 in the audit):
//   - Refuses any DownloadURL / ChecksumURL whose scheme is not https or
//     whose host is not in allowedUpdateHosts (prevents MITM / compromised
//     GitHub API redirecting us to an attacker host).
//   - Strips path components from FileName via filepath.Base before joining
//     it into the temp directory (prevents a crafted asset name from
//     escaping the temp dir and writing into e.g. System32).
//   - Stages the download in a private 0700 temp directory created with
//     os.MkdirTemp (eliminates the /tmp TOCTOU race where a local attacker
//     could swap the file between chmod and exec).
//   - Verifies the downloaded bytes against the SHA-256 hash published in
//     the release's SHA256SUMS asset. No verified checksum ⇒ no install.
func DownloadAndInstall(info *UpdateInfo) error {
	// Re-entry guard (H-UPD-1). A user double-clicking "Update Now"
	// used to launch two parallel downloads and two UAC prompts; the
	// second install could truncate the installer file the first was
	// still writing to. CompareAndSwap is single-owner — the first
	// call wins, the second sees the flag already set and bails.
	if !installInProgress.CompareAndSwap(false, true) {
		return fmt.Errorf("an update installation is already in progress")
	}
	defer installInProgress.Store(false)

	if info == nil || !info.Available || info.DownloadURL == "" {
		return fmt.Errorf("no update available")
	}
	if info.ChecksumURL == "" {
		return fmt.Errorf("refusing to install update: release is missing %s — cannot verify integrity", ChecksumAssetName)
	}

	dlURL, err := security.ValidateHTTPSURL(info.DownloadURL, allowedUpdateHosts)
	if err != nil {
		return fmt.Errorf("download URL rejected: %w", err)
	}
	csURL, err := security.ValidateHTTPSURL(info.ChecksumURL, allowedUpdateHosts)
	if err != nil {
		return fmt.Errorf("checksum URL rejected: %w", err)
	}

	safeName := filepath.Base(info.FileName)
	if safeName == "." || safeName == ".." || safeName == "" || safeName != info.FileName {
		return fmt.Errorf("invalid filename in update asset: %q", info.FileName)
	}

	// Private temp directory: os.MkdirTemp creates with 0700 on Unix, so
	// other local users cannot read or substitute the installer between
	// download and launch.
	tempDir, err := os.MkdirTemp("", "taskflow-update-*")
	if err != nil {
		return fmt.Errorf("failed to create temp dir: %w", err)
	}
	// Clean up on every failure path. On success we deliberately leave the
	// file in place: installUpdate returns before the installer has actually
	// consumed it, so removing the directory here would race the installer.
	succeeded := false
	defer func() {
		if !succeeded {
			_ = os.RemoveAll(tempDir)
		}
	}()

	destPath := filepath.Join(tempDir, safeName)
	log.Printf("Downloading update v%s from %s", info.Version, dlURL.String())
	if err := downloadToFile(dlURL.String(), destPath); err != nil {
		return fmt.Errorf("download failed: %w", err)
	}

	expectedHash, err := fetchExpectedChecksum(csURL.String(), safeName)
	if err != nil {
		return fmt.Errorf("failed to obtain checksum: %w", err)
	}

	if err := verifyChecksum(destPath, expectedHash); err != nil {
		return fmt.Errorf("integrity check failed: %w", err)
	}

	log.Printf("Verified %s (sha256=%s), launching installer...", safeName, expectedHash)

	// Launch the platform-specific installer. The caller is responsible for
	// triggering a graceful app shutdown (runtime.Quit) so in-flight activity
	// heartbeats and background services are stopped cleanly before the
	// installer replaces the running binary.
	if err := installUpdate(destPath); err != nil {
		return fmt.Errorf("failed to install update: %w", err)
	}
	succeeded = true
	return nil
}

// downloadToFile GETs url with a strict redirect policy (https + allowlisted
// host only) and streams the body into destPath.
func downloadToFile(rawURL, destPath string) error {
	client := &http.Client{
		Timeout: 5 * time.Minute,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if _, err := security.ValidateHTTPSURL(req.URL.String(), allowedUpdateHosts); err != nil {
				return fmt.Errorf("redirect refused: %w", err)
			}
			if len(via) >= 10 {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}
	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", UserAgent)

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	file, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("create file: %w", err)
	}
	_, err = io.Copy(file, resp.Body)
	if cerr := file.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return fmt.Errorf("write: %w", err)
	}
	return nil
}

// fetchExpectedChecksum downloads a SHA256SUMS file (same format as
// `sha256sum`: "<hex>  <filename>" per line) and returns the hex hash for
// the requested filename.
func fetchExpectedChecksum(rawURL, fileName string) (string, error) {
	client := &http.Client{
		Timeout: 30 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if _, err := security.ValidateHTTPSURL(req.URL.String(), allowedUpdateHosts); err != nil {
				return fmt.Errorf("redirect refused: %w", err)
			}
			if len(via) >= 10 {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}
	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", UserAgent)

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("HTTP %d fetching checksums", resp.StatusCode)
	}

	// Cap the read at 64 KiB — SHA256SUMS is tiny; anything larger is
	// either malformed or an attacker trying to exhaust memory.
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return "", err
	}

	return parseSHA256SUMS(string(body), fileName)
}

// parseSHA256SUMS walks sha256sum-style output looking for an entry that
// matches fileName. Split from fetchExpectedChecksum so it can be tested
// without standing up an HTTPS server.
//
// Accepted line formats (sha256sum compatible):
//
//	<64-hex>  <filename>       (text mode)
//	<64-hex> *<filename>       (binary mode — leading '*' is stripped)
//	<64-hex>  ./subdir/<name>  (leading path — compared via filepath.Base)
//
// Blank lines and lines starting with '#' are ignored.
func parseSHA256SUMS(body, fileName string) (string, error) {
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Lenient about whitespace between hash and filename.
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		hash := fields[0]
		// The filename may be preceded by `*` (binary mode) and may
		// include a leading path — compare against basename only.
		name := strings.TrimPrefix(fields[len(fields)-1], "*")
		if filepath.Base(name) == fileName {
			if len(hash) != 64 {
				return "", fmt.Errorf("malformed hash for %q: length %d", fileName, len(hash))
			}
			return strings.ToLower(hash), nil
		}
	}
	return "", fmt.Errorf("no checksum entry for %q", fileName)
}

// verifyChecksum streams filePath through SHA-256 and compares the result to
// expectedHex using constant-time comparison.
func verifyChecksum(filePath, expectedHex string) error {
	f, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	got := hex.EncodeToString(h.Sum(nil))
	if subtle.ConstantTimeCompare([]byte(got), []byte(strings.ToLower(expectedHex))) != 1 {
		return fmt.Errorf("sha256 mismatch: got %s, expected %s", got, expectedHex)
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
