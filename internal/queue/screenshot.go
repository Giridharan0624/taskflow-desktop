package queue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// screenshotMaxEntries caps the screenshot backlog. Screenshots are
// much bigger than heartbeats (50–200 KB JPEG each) so the cap is
// lower: ~240 shots ≈ 40 hours of 10-min-interval captures at 150
// KB avg ≈ 36 MB disk. Beyond that, evict oldest.
const screenshotMaxEntries = 240

// ScreenshotEntry is the unmarshaled metadata for a queued frame.
// The actual JPEG bytes live in a sibling file; kept apart so we
// don't pull every frame into RAM when listing the queue.
type ScreenshotEntry struct {
	Filename string // original filename the caller passed (used for the S3 key)
	JPEGPath string // absolute path to the .jpg on disk
}

// ScreenshotQueue persists pending screenshot uploads. A failed
// upload (network down, presign expired, S3 slow) stays on disk so
// the next drain tick retries. Pair with HeartbeatQueue: a
// screenshot flushes the activity bucket it was captured during,
// and both should survive the same outage window.
type ScreenshotQueue struct {
	lockedDir
}

// NewScreenshotQueue resolves the directory and creates it if
// missing.
func NewScreenshotQueue() (*ScreenshotQueue, error) {
	root, err := BaseDir()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(root, "screenshots")
	return &ScreenshotQueue{lockedDir: lockedDir{dir: dir}}, nil
}

// Enqueue persists JPEG bytes + sidecar metadata. The filename is
// what eventually lands as the S3 key suffix — we keep it verbatim
// so a re-upload hits the same key (S3 overwrites, dedupes for
// free).
func (q *ScreenshotQueue) Enqueue(jpeg []byte, filename string) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	id := entryID(time.Now())
	jpegName := id + ".jpg"
	metaName := id + ".meta.json"

	meta := map[string]string{"filename": filename}
	metaBytes, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("screenshot meta marshal: %w", err)
	}

	// JPEG first, meta second. If the JPEG write succeeds but meta
	// fails, a stray .jpg without sidecar is cleaned up by
	// drainOnce below (we skip entries missing a partner). If meta
	// succeeds without JPEG, same deal.
	if err := writeAtomic(q.dir, jpegName, jpeg); err != nil {
		return fmt.Errorf("screenshot enqueue jpeg: %w", err)
	}
	if err := writeAtomic(q.dir, metaName, metaBytes); err != nil {
		_ = os.Remove(filepath.Join(q.dir, jpegName))
		return fmt.Errorf("screenshot enqueue meta: %w", err)
	}

	// Trim pairs: one logical entry is two files, so we halve the
	// cap (trimOldest counts files).
	trimOldest(q.dir, screenshotMaxEntries*2)
	return nil
}

// Drain replays queued screenshots. For each paired entry we call
// `upload(jpeg, filename)`; on success both files are deleted. On
// failure we stop (same stop-first-failure contract as
// HeartbeatQueue).
//
// The S3 upload path is already idempotent: same filename ⇒ same
// key ⇒ overwrite. So a replayed screenshot that the server
// actually received the first time is a no-op bandwidth spend, not
// a correctness problem.
func (q *ScreenshotQueue) Drain(ctx context.Context, upload func(jpeg []byte, filename string) error) int {
	q.mu.Lock()
	defer q.mu.Unlock()

	names, err := listSorted(q.dir)
	if err != nil {
		log.Printf("screenshot drain list: %v", err)
		return 0
	}
	sent := 0
	// Group by id prefix (before `.jpg` or `.meta.json`).
	for _, name := range names {
		select {
		case <-ctx.Done():
			return sent
		default:
		}
		if !strings.HasSuffix(name, ".meta.json") {
			continue
		}
		id := strings.TrimSuffix(name, ".meta.json")
		metaPath := filepath.Join(q.dir, id+".meta.json")
		jpegPath := filepath.Join(q.dir, id+".jpg")

		metaBytes, err := os.ReadFile(metaPath)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			log.Printf("screenshot drain read meta %s: %v", metaPath, err)
			return sent
		}
		var meta struct {
			Filename string `json:"filename"`
		}
		if err := json.Unmarshal(metaBytes, &meta); err != nil {
			log.Printf("screenshot drain: dropping corrupt meta %s: %v", metaPath, err)
			_ = os.Remove(metaPath)
			_ = os.Remove(jpegPath)
			continue
		}

		jpeg, err := os.ReadFile(jpegPath)
		if errors.Is(err, os.ErrNotExist) {
			// Half-written pair — drop meta too and move on.
			log.Printf("screenshot drain: orphan meta %s (no jpeg) — dropping", id)
			_ = os.Remove(metaPath)
			continue
		}
		if err != nil {
			return sent
		}

		if err := upload(jpeg, meta.Filename); err != nil {
			log.Printf("screenshot drain stopped after %d sent: %v", sent, err)
			return sent
		}
		if err := os.Remove(jpegPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			log.Printf("screenshot drain: remove jpeg %s: %v", jpegPath, err)
			return sent
		}
		if err := os.Remove(metaPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			log.Printf("screenshot drain: remove meta %s: %v", metaPath, err)
			return sent
		}
		sent++
	}
	return sent
}

// Count returns the current backlog size (logical entries, not
// files — divide the on-disk file count by 2).
func (q *ScreenshotQueue) Count() int {
	return q.count() / 2
}
