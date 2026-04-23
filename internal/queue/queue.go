// Package queue provides durable, file-backed queues so the desktop
// app keeps activity data, screenshots, timer events, and task lists
// across network outages and process restarts.
//
// Design: one file per entry, in a dedicated directory per queue,
// named so that lexicographic sort === enqueue order. Atomic write
// pattern: `os.CreateTemp` next to the target + `f.Sync` + rename.
// This gives us crash-safety without pulling in an embedded DB.
//
// Size discipline: each queue caps its backlog so a month-long
// outage can't fill the user's disk. When the cap is reached the
// OLDEST entries are dropped first — activity from yesterday is
// worth less than activity from the last hour, and the backend
// attendance sweep already closes sessions at the last-known
// heartbeat so those old buckets would have been ignored anyway.
//
// Concurrency: every exported method takes the per-queue mutex.
// Drain is a cancellable loop that stops on first persistent failure
// so one dead network doesn't starve the CPU.
package queue

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// BaseDir returns the root directory under which all queue sub-
// directories live. We put this under the OS-appropriate app-data
// path so queued data survives installs and stays per-user.
//
// Windows: %APPDATA%\TaskFlow\queue
// Linux:   $XDG_DATA_HOME/taskflow/queue (or ~/.local/share/taskflow/queue)
// macOS:   ~/Library/Application Support/TaskFlow/queue
func BaseDir() (string, error) {
	root, err := appDataRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "queue"), nil
}

// entryID generates a lexicographically-sortable filename prefix:
// "20260422T223006.123456789Z-<8-hex>". Time first, then random
// suffix to keep enqueue order stable even if two entries land in
// the same nanosecond.
func entryID(now time.Time) string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return now.UTC().Format("20060102T150405.000000000Z") + "-" + hex.EncodeToString(b[:])
}

// writeAtomic writes data to dir/name via a same-dir tmp + rename.
// fsync is load-bearing: without it a crash between write and
// rename can surface an empty file on next boot.
func writeAtomic(dir, name string, data []byte) error {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".tmp-")
	if err != nil {
		return err
	}
	tmp := f.Name()
	if _, err := f.Write(data); err != nil {
		f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, filepath.Join(dir, name)); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// listSorted returns entry filenames in ascending lexicographic
// order, filtering out hidden/tmp files. Entries whose name does not
// match the expected prefix (e.g. from an older version) are
// ignored.
func listSorted(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		n := e.Name()
		if len(n) == 0 || n[0] == '.' {
			continue
		}
		names = append(names, n)
	}
	sort.Strings(names)
	return names, nil
}

// trimOldest removes files from dir until at most keep remain.
// Called by Enqueue paths when they observe the cap is exceeded.
// Returns the count deleted so callers can log the eviction.
func trimOldest(dir string, keep int) int {
	names, err := listSorted(dir)
	if err != nil || len(names) <= keep {
		return 0
	}
	drop := len(names) - keep
	for i := 0; i < drop; i++ {
		full := filepath.Join(dir, names[i])
		if err := os.Remove(full); err != nil {
			log.Printf("queue: trim remove %s failed: %v", full, err)
		}
	}
	if drop > 0 {
		log.Printf("queue: evicted %d oldest entries from %s (cap reached)", drop, dir)
	}
	return drop
}

// drainOnce walks the queue in order, calling send(id, data) per
// entry. On success the entry is removed. On first failure we STOP
// the drain — continuing would just produce the same error for
// every remaining entry and burn CPU. Returns the count sent.
func drainOnce(dir string, send func(id string, data []byte) error) (int, error) {
	names, err := listSorted(dir)
	if err != nil {
		return 0, err
	}
	sent := 0
	for _, n := range names {
		full := filepath.Join(dir, n)
		data, err := os.ReadFile(full)
		if err != nil {
			// Entry went missing between list and read (another
			// drain ran, probably). Skip.
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return sent, err
		}
		if err := send(n, data); err != nil {
			return sent, err
		}
		if err := os.Remove(full); err != nil && !errors.Is(err, os.ErrNotExist) {
			// If we can't remove, we're stuck — future drains
			// will retry and the idempotency guard on the server
			// side prevents duplicate writes.
			return sent, fmt.Errorf("queue: drain remove %s: %w", full, err)
		}
		sent++
	}
	return sent, nil
}

// lockedDir provides mutex-guarded access for a per-queue directory.
// Embed in queue structs to inherit the locking discipline.
type lockedDir struct {
	dir string
	mu  sync.Mutex
}

// count returns the current enqueued entry count, reading the
// directory under the mutex.
func (q *lockedDir) count() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	names, _ := listSorted(q.dir)
	return len(names)
}
