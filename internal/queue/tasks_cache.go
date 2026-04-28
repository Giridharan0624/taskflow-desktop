package queue

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// TasksCache persists the last successful /users/me/tasks response
// so the user can pick a task and start the timer while offline.
// On reconnect the live fetch overwrites the cache.
//
// Stored as a single JSON file — no versioning, no rotation. The
// worst case is we return a task list from last week; the user
// corrects it after the live fetch lands (seconds later).
type TasksCache struct {
	mu   sync.Mutex
	path string
}

// NewTasksCache creates the cache (and the parent directory) if
// needed.
func NewTasksCache() (*TasksCache, error) {
	root, err := appDataRoot()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(root, "cache")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	return &TasksCache{path: filepath.Join(dir, "tasks.json")}, nil
}

// Store atomically overwrites the cache with `data`. Pass the raw
// JSON body from the API response — keeps us unmarshalled-free and
// insulates the cache from struct-version drift.
func (c *TasksCache) Store(data []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	dir := filepath.Dir(c.path)
	return writeAtomic(dir, filepath.Base(c.path), data)
}

// Load returns the cached bytes, or nil if the cache is empty /
// missing. Missing is not an error (fresh install, cleared cache).
func (c *TasksCache) Load() ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	data, err := os.ReadFile(c.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("tasks cache read: %w", err)
	}
	return data, nil
}

// Clear removes the cache file. Called by the "Clear local cache"
// settings action. Idempotent — missing file is not an error.
func (c *TasksCache) Clear() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := os.Remove(c.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// LoadInto unmarshals the cached JSON into `out`. Returns false if
// the cache is empty; a corrupt cache is treated as empty and
// silently removed so the next successful Store replaces it.
func (c *TasksCache) LoadInto(out any) (bool, error) {
	data, err := c.Load()
	if err != nil {
		return false, err
	}
	if data == nil {
		return false, nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		c.mu.Lock()
		_ = os.Remove(c.path)
		c.mu.Unlock()
		return false, nil
	}
	return true, nil
}
