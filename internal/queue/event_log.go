package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Event is a timer-session state change that the backend needs to
// know about. These are deliberately coarse: SignIn / SignOut /
// TaskSwitched. Fine-grained activity lives in the heartbeat queue.
//
// On reconnect, the drain replays events in order. The backend's
// /attendance endpoints are already idempotent via the per-day
// session record (signing in twice is a no-op; signing out twice
// closes a closed session cleanly) so there's no dedupe key
// needed here — we just replay and let the server absorb dupes.
type Event struct {
	Timestamp   string                 `json:"timestamp"`   // RFC3339 UTC
	Kind        string                 `json:"kind"`        // "sign_in" | "sign_out" | "task_switched"
	Payload     map[string]interface{} `json:"payload,omitempty"`
}

const (
	EventSignIn        = "sign_in"
	EventSignOut       = "sign_out"
	EventTaskSwitched  = "task_switched"
)

// eventLogMaxEntries caps the backlog. 500 events ≈ months of
// active use at typical cadence; beyond that we evict oldest.
const eventLogMaxEntries = 500

// EventLog is an append-only store of timer-session state changes.
// One file per event so the ordered-drain pattern stays consistent
// with Heartbeat and Screenshot queues.
type EventLog struct {
	mu  sync.Mutex
	dir string
}

// NewEventLog creates the log directory if missing.
func NewEventLog() (*EventLog, error) {
	root, err := BaseDir()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(root, "events")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	return &EventLog{dir: dir}, nil
}

// Append records a new event. Called from Wails bindings (SignIn /
// SignOut) immediately before the network call, so a crash or
// network loss between here and the server still leaves an audit
// trail for replay.
func (l *EventLog) Append(kind string, payload map[string]interface{}) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	ev := Event{
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		Kind:      kind,
		Payload:   payload,
	}
	data, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("event marshal: %w", err)
	}
	id := entryID(time.Now()) + ".json"
	if err := writeAtomic(l.dir, id, data); err != nil {
		return fmt.Errorf("event append: %w", err)
	}
	trimOldest(l.dir, eventLogMaxEntries)
	return nil
}

// DrainHandler is called for each replayed event in order. It
// should return nil on success (or when the event is safely
// replay-acknowledged by the server) and a non-nil error to stop
// the drain.
type DrainHandler func(ev Event) error

// Drain replays buffered events in FIFO order. On handler error we
// stop and leave the remaining events queued for the next tick.
func (l *EventLog) Drain(ctx context.Context, handle DrainHandler) int {
	l.mu.Lock()
	defer l.mu.Unlock()

	sent, err := drainOnce(l.dir, func(id string, data []byte) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		var ev Event
		if err := json.Unmarshal(data, &ev); err != nil {
			log.Printf("event log: dropping corrupt entry %s: %v", id, err)
			return nil
		}
		return handle(ev)
	})
	if err != nil {
		log.Printf("event log drain stopped after %d sent: %v", sent, err)
	}
	return sent
}

// Count returns the pending replay count.
func (l *EventLog) Count() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	names, _ := listSorted(l.dir)
	return len(names)
}

// HasPending reports whether at least one event is queued. Cheaper
// than Count when you just need the boolean for a UI banner.
func (l *EventLog) HasPending() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	entries, err := os.ReadDir(l.dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			return true
		}
	}
	return false
}
