// Package workspace stores the desktop's selected tenant slug between
// launches, written to ~/.taskflow/workspace.json.
//
// Phase 6 of the SaaS migration: a single signed binary needs to remember
// which tenant the user belongs to without re-asking on every launch, but
// also without baking it into the build like the legacy single-tenant
// flow did. The slug here is sent to the backend as the `x-org-slug`
// header so backend can correlate logs to a tenant; the canonical org_id
// still rides in the JWT's `custom:orgId` claim and is what every handler
// actually authorizes against.
package workspace

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

// File layout — kept tiny so a future "switch workspace" tray action can
// rewrite it atomically. Versioned so a v2 reader can refuse a v3 file
// instead of silently misinterpreting it.
type Workspace struct {
	Version int    `json:"version"`
	Slug    string `json:"slug"`
}

const (
	currentVersion = 1
	dirName        = ".taskflow"
	fileName       = "workspace.json"
)

// slugPattern mirrors the backend's slug rules: 3–30 chars, lowercase
// alphanumeric + hyphens, must start and end with alphanumeric. Anything
// else is rejected before it can be persisted, so a corrupted file or a
// stray space can never reach the API as a header value.
var slugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,28}[a-z0-9]$`)

// ErrNoWorkspace is returned by Load when no workspace has been chosen
// yet (first launch, or the file was deleted by a "Switch workspace"
// action). Callers distinguish this from real I/O errors so the UI can
// route to the first-run prompt instead of an error toast.
var ErrNoWorkspace = errors.New("no workspace configured")

var (
	muOverride sync.RWMutex
	override   string // test hook — set via SetPathForTest
)

// path returns the absolute path to the workspace.json. Computed lazily
// so test code can override the home dir without touching env vars.
func path() (string, error) {
	muOverride.RLock()
	if override != "" {
		p := override
		muOverride.RUnlock()
		return p, nil
	}
	muOverride.RUnlock()

	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, dirName, fileName), nil
}

// SetPathForTest overrides the workspace path. Pass "" to clear.
func SetPathForTest(p string) {
	muOverride.Lock()
	override = p
	muOverride.Unlock()
}

// Load returns the saved workspace slug, or ErrNoWorkspace if none.
// Returns a generic error for any I/O or parse failure so the caller can
// decide whether to surface or wipe-and-reprompt.
func Load() (string, error) {
	p, err := path()
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return "", ErrNoWorkspace
		}
		return "", err
	}
	var ws Workspace
	if err := json.Unmarshal(data, &ws); err != nil {
		// A corrupt file is treated as "no workspace" so the user can
		// re-enter rather than being locked out by a one-byte truncation.
		return "", ErrNoWorkspace
	}
	if ws.Version != currentVersion {
		return "", ErrNoWorkspace
	}
	slug := strings.ToLower(strings.TrimSpace(ws.Slug))
	if !slugPattern.MatchString(slug) {
		return "", ErrNoWorkspace
	}
	return slug, nil
}

// Save writes the slug to disk. Validates the slug shape and rejects
// anything else — invalid input never reaches disk and never reaches the
// API as an `x-org-slug` header.
func Save(slug string) error {
	clean := strings.ToLower(strings.TrimSpace(slug))
	if !slugPattern.MatchString(clean) {
		return errors.New("invalid workspace slug")
	}
	p, err := path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(Workspace{Version: currentVersion, Slug: clean})
	if err != nil {
		return err
	}
	// Write to a temp file then rename so a crash mid-write cannot leave
	// a half-written workspace.json that Load would treat as corrupt.
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// Clear removes the saved workspace. Safe to call when no workspace
// exists — returns nil, not an error, so the caller doesn't have to
// check existence first.
func Clear() error {
	p, err := path()
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return nil
}
