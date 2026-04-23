package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"path/filepath"
	"time"
)

// heartbeatMaxEntries caps the heartbeat backlog. 5-min buckets ⇒
// 3000 entries ≈ 10 days of continuous offline tracking. Beyond
// that the backend attendance sweep has long since closed the
// session, so older buckets would be orphaned anyway.
const heartbeatMaxEntries = 3000

// HeartbeatQueue persists ActivityMonitor heartbeat buckets so a
// network outage doesn't drop keystroke/mouse counts. Enqueue is
// called from the activity goroutine immediately after computing a
// bucket; Drain runs on a separate ticker and replays the backlog
// whenever the network is reachable.
//
// Idempotency contract with the backend: the bucket's `timestamp`
// field is the dedupe key. We never rewrite it when replaying, so a
// heartbeat that was in-flight when the network blipped (and the
// server already recorded) is safely re-sent and dropped server-
// side on the second hit. See the idempotency check in
// backend/src/contexts/activity/application/use_cases.py.
type HeartbeatQueue struct {
	lockedDir
}

// NewHeartbeatQueue resolves the queue directory and creates it if
// missing. Safe to call multiple times — any existing entries from a
// previous run remain queued until drained successfully.
func NewHeartbeatQueue() (*HeartbeatQueue, error) {
	root, err := BaseDir()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(root, "heartbeats")
	return &HeartbeatQueue{lockedDir: lockedDir{dir: dir}}, nil
}

// Enqueue serializes `bucket` as JSON and writes it atomically to
// the queue directory. Called from the ActivityMonitor after it
// snapshots + resets the in-memory bucket.
//
// The bucket's "timestamp" field is expected to already be set by
// the caller; we don't touch it so a replayed bucket keeps the same
// key and the backend idempotency guard catches duplicates.
func (q *HeartbeatQueue) Enqueue(bucket map[string]interface{}) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	data, err := json.Marshal(bucket)
	if err != nil {
		return fmt.Errorf("heartbeat marshal: %w", err)
	}
	id := entryID(time.Now()) + ".json"
	if err := writeAtomic(q.dir, id, data); err != nil {
		return fmt.Errorf("heartbeat enqueue: %w", err)
	}
	// Enforce the cap AFTER the write succeeds: better to drop the
	// oldest than to reject the newest (the newest is what the
	// user is generating right now).
	trimOldest(q.dir, heartbeatMaxEntries)
	return nil
}

// Drain iterates the queue in FIFO order, calling `send` for each
// bucket. On send success the entry is deleted; on send failure we
// stop (avoids hot-looping against a dead network). Returns the
// count successfully sent.
//
// Drain is safe to call concurrently with Enqueue: the per-queue
// mutex serialises the two paths. In practice only one drain
// worker runs at a time.
func (q *HeartbeatQueue) Drain(ctx context.Context, send func(bucket map[string]interface{}) error) int {
	q.mu.Lock()
	defer q.mu.Unlock()

	sent, err := drainOnce(q.dir, func(id string, data []byte) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		var bucket map[string]interface{}
		if err := json.Unmarshal(data, &bucket); err != nil {
			// A corrupt entry would otherwise pin the drain
			// forever; log it and treat as "consumed" so the
			// caller can move on. See V3-offline-M1.
			log.Printf("heartbeat queue: dropping corrupt entry %s: %v", id, err)
			return nil
		}
		return send(bucket)
	})
	if err != nil {
		log.Printf("heartbeat drain stopped after %d sent: %v", sent, err)
	}
	return sent
}

// Count returns the current backlog size. Useful for diagnostics
// and to surface an "N pending" banner in the UI.
func (q *HeartbeatQueue) Count() int {
	return q.count()
}
