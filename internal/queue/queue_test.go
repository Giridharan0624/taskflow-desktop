package queue

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// withTempBaseDir redirects appDataRoot to a t.TempDir so tests
// never touch the user's real %APPDATA%. Needed because BaseDir is
// derived from the OS and the constructors assemble paths under it.
func withTempBaseDir(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	// Set the OS-specific env var the path-resolver consults so the
	// queue constructors end up under tmp. We set all three so the
	// same test file works for every GOOS without build tags.
	t.Setenv("APPDATA", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)
	t.Setenv("HOME", tmp)
	return tmp
}

func TestHeartbeatQueue_EnqueueDrainRoundTrip(t *testing.T) {
	withTempBaseDir(t)
	q, err := NewHeartbeatQueue()
	if err != nil {
		t.Fatalf("NewHeartbeatQueue: %v", err)
	}

	bucket := map[string]interface{}{
		"timestamp":      "2026-04-22T22:30:00Z",
		"keyboard_count": float64(42),
	}
	if err := q.Enqueue(bucket); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if got := q.Count(); got != 1 {
		t.Fatalf("Count after enqueue: got %d want 1", got)
	}

	var received map[string]interface{}
	sent := q.Drain(context.Background(), func(b map[string]interface{}) error {
		received = b
		return nil
	})
	if sent != 1 {
		t.Fatalf("Drain sent: got %d want 1", sent)
	}
	if got := q.Count(); got != 0 {
		t.Fatalf("Count after drain: got %d want 0", got)
	}
	if received["keyboard_count"].(float64) != 42 {
		t.Errorf("roundtrip mismatch: got %#v", received)
	}
}

func TestHeartbeatQueue_DrainStopsOnFirstError(t *testing.T) {
	withTempBaseDir(t)
	q, _ := NewHeartbeatQueue()
	for i := 0; i < 3; i++ {
		q.Enqueue(map[string]interface{}{"timestamp": "t", "i": i})
	}
	calls := 0
	sent := q.Drain(context.Background(), func(b map[string]interface{}) error {
		calls++
		return errors.New("network down")
	})
	if sent != 0 {
		t.Fatalf("Drain sent: got %d want 0 (first call failed)", sent)
	}
	if calls != 1 {
		t.Errorf("handler called %d times, want 1 (stop on first failure)", calls)
	}
	if q.Count() != 3 {
		t.Errorf("all 3 entries should still be queued, got %d", q.Count())
	}
}

func TestHeartbeatQueue_SurvivesCorruptEntry(t *testing.T) {
	withTempBaseDir(t)
	q, _ := NewHeartbeatQueue()
	// Drop a junk file directly into the queue dir to simulate
	// corruption from a previous crash.
	if err := os.MkdirAll(q.dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(q.dir, "20000101T000000.000000000Z-deadbeef.json"), []byte("not json"), 0600); err != nil {
		t.Fatal(err)
	}
	q.Enqueue(map[string]interface{}{"timestamp": "t", "good": true})

	callbackCalls := 0
	sent := q.Drain(context.Background(), func(b map[string]interface{}) error {
		callbackCalls++
		// Only the good bucket reaches the send callback —
		// corrupt entries are dropped before callback.
		if b["good"] != true {
			t.Errorf("unexpected payload %#v", b)
		}
		return nil
	})
	// Drain's `sent` counts both legit sends AND dropped-as-
	// corrupt entries because drainOnce treats the "return nil"
	// from the corrupt-handling branch as successful consumption.
	// What matters is: queue ends empty and the good bucket was
	// delivered exactly once.
	if callbackCalls != 1 {
		t.Errorf("send callback called %d times, want 1", callbackCalls)
	}
	if sent != 2 {
		t.Errorf("sent=%d want 2 (1 legit + 1 corrupt-dropped)", sent)
	}
	if q.Count() != 0 {
		t.Errorf("queue should be empty, got %d", q.Count())
	}
}

func TestHeartbeatQueue_ConcurrentEnqueue(t *testing.T) {
	withTempBaseDir(t)
	q, _ := NewHeartbeatQueue()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_ = q.Enqueue(map[string]interface{}{"i": i})
		}(i)
	}
	wg.Wait()
	if got := q.Count(); got != 20 {
		t.Errorf("Count after 20 concurrent Enqueue: got %d want 20", got)
	}
}

func TestScreenshotQueue_EnqueueDrainRoundTrip(t *testing.T) {
	withTempBaseDir(t)
	q, err := NewScreenshotQueue()
	if err != nil {
		t.Fatalf("NewScreenshotQueue: %v", err)
	}
	jpeg := []byte{0xff, 0xd8, 0xff, 0xe0, 'J', 'F', 'I', 'F'}
	// Pre-pair-schema callers pass nil for bucket — covers backward compat.
	if err := q.Enqueue(jpeg, "screenshot_test.jpg", nil); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if q.Count() != 1 {
		t.Errorf("Count after enqueue: got %d want 1", q.Count())
	}

	var gotJPEG []byte
	var gotName string
	var gotBucket map[string]interface{}
	sent := q.Drain(context.Background(), func(jpegBytes []byte, name string, bucket map[string]interface{}) error {
		gotJPEG = jpegBytes
		gotName = name
		gotBucket = bucket
		return nil
	})
	if sent != 1 {
		t.Fatalf("Drain sent: got %d want 1", sent)
	}
	if string(gotJPEG) != string(jpeg) {
		t.Errorf("jpeg roundtrip mismatch")
	}
	if gotName != "screenshot_test.jpg" {
		t.Errorf("filename roundtrip: got %q want %q", gotName, "screenshot_test.jpg")
	}
	if gotBucket != nil {
		t.Errorf("bucket roundtrip: got %v want nil for legacy enqueue", gotBucket)
	}
}

// TestScreenshotQueue_BucketPairing covers the V3-orphan fix: the
// activity bucket captured at screenshot time must round-trip through
// the queue so the drain worker can re-link it to the recovered S3
// URL on retry.
func TestScreenshotQueue_BucketPairing(t *testing.T) {
	withTempBaseDir(t)
	q, err := NewScreenshotQueue()
	if err != nil {
		t.Fatalf("NewScreenshotQueue: %v", err)
	}
	jpeg := []byte{0xff, 0xd8, 0xff}
	bucket := map[string]interface{}{
		"timestamp":      "2026-04-27T10:00:00Z",
		"keyboard_count": float64(42), // JSON unmarshal coerces ints to float64
		"mouse_count":    float64(13),
		"top_app":        "code",
	}
	if err := q.Enqueue(jpeg, "linked.jpg", bucket); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	var gotBucket map[string]interface{}
	sent := q.Drain(context.Background(), func(_ []byte, _ string, b map[string]interface{}) error {
		gotBucket = b
		return nil
	})
	if sent != 1 {
		t.Fatalf("Drain sent: got %d want 1", sent)
	}
	if gotBucket == nil {
		t.Fatalf("bucket lost in roundtrip")
	}
	if gotBucket["timestamp"] != "2026-04-27T10:00:00Z" {
		t.Errorf("bucket timestamp roundtrip: got %v", gotBucket["timestamp"])
	}
	if gotBucket["keyboard_count"] != float64(42) {
		t.Errorf("bucket keyboard_count roundtrip: got %v", gotBucket["keyboard_count"])
	}
}

func TestTasksCache_StoreLoad(t *testing.T) {
	withTempBaseDir(t)
	c, err := NewTasksCache()
	if err != nil {
		t.Fatalf("NewTasksCache: %v", err)
	}

	type task struct {
		TaskID string `json:"taskId"`
		Title  string `json:"title"`
	}
	in := []task{{TaskID: "t1", Title: "Fix bug"}, {TaskID: "t2", Title: "Ship feature"}}
	body, _ := json.Marshal(in)
	if err := c.Store(body); err != nil {
		t.Fatalf("Store: %v", err)
	}

	var out []task
	ok, err := c.LoadInto(&out)
	if err != nil {
		t.Fatalf("LoadInto: %v", err)
	}
	if !ok || len(out) != 2 || out[0].Title != "Fix bug" {
		t.Errorf("LoadInto got %#v (ok=%v)", out, ok)
	}
}

func TestEventLog_AppendDrain(t *testing.T) {
	withTempBaseDir(t)
	l, err := NewEventLog()
	if err != nil {
		t.Fatalf("NewEventLog: %v", err)
	}
	if err := l.Append(EventSignIn, map[string]interface{}{"task_id": "t1"}); err != nil {
		t.Fatal(err)
	}
	if err := l.Append(EventSignOut, nil); err != nil {
		t.Fatal(err)
	}
	if l.Count() != 2 {
		t.Errorf("Count: got %d want 2", l.Count())
	}

	var kinds []string
	sent := l.Drain(context.Background(), func(ev Event) error {
		kinds = append(kinds, ev.Kind)
		return nil
	})
	if sent != 2 {
		t.Errorf("Drain sent: got %d want 2", sent)
	}
	if strings.Join(kinds, ",") != EventSignIn+","+EventSignOut {
		t.Errorf("replay order wrong: %v", kinds)
	}
	if l.Count() != 0 {
		t.Errorf("Count after drain: got %d want 0", l.Count())
	}
}

func TestEntryID_LexicographicallyOrdered(t *testing.T) {
	// Two calls in quick succession must produce strings that sort
	// in chronological order. Otherwise listSorted's FIFO property
	// breaks and drains would deliver out of order.
	t1 := time.Date(2026, 4, 22, 0, 0, 0, 1, time.UTC)
	t2 := time.Date(2026, 4, 22, 0, 0, 0, 2, time.UTC)
	a := entryID(t1)
	b := entryID(t2)
	if !(a < b) {
		t.Errorf("entryID not sortable: a=%q b=%q", a, b)
	}
}
