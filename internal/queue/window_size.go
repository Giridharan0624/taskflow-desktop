package queue

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
)

// WindowSize captures the user's preferred window dimensions so the
// next launch restores it. Stored as a tiny JSON file in the app-data
// cache directory — same disk-write atomicity as TasksCache.
type WindowSize struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

// WindowSizeStore is the disk-backed handle. Best-effort: any
// load/save error is logged at the call site and the app falls back
// to compiled-in defaults rather than failing startup.
type WindowSizeStore struct {
	mu   sync.Mutex
	path string
}

// NewWindowSizeStore creates the cache dir if missing and returns a
// store. Same lifecycle pattern as TasksCache.
func NewWindowSizeStore() (*WindowSizeStore, error) {
	root, err := appDataRoot()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(root, "cache")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	return &WindowSizeStore{path: filepath.Join(dir, "window-size.json")}, nil
}

// Save atomically writes the dimensions to disk. Sub-1 KB JSON, so
// the writeAtomic temp+rename pattern is overkill but consistent
// with the rest of the queue package.
func (s *WindowSizeStore) Save(width, height int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := json.Marshal(WindowSize{Width: width, Height: height})
	if err != nil {
		return err
	}
	return writeAtomic(filepath.Dir(s.path), filepath.Base(s.path), data)
}

// Load returns the persisted dimensions, or (zero, false) when the
// file is missing / corrupt. A corrupt file is silently removed so
// the next Save replaces it.
func (s *WindowSizeStore) Load() (WindowSize, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return WindowSize{}, false
	}
	if err != nil {
		return WindowSize{}, false
	}
	var ws WindowSize
	if err := json.Unmarshal(data, &ws); err != nil {
		_ = os.Remove(s.path)
		return WindowSize{}, false
	}
	return ws, true
}
