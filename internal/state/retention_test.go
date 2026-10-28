package state

import (
	"bytes"
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/mxhook/mxhook/queue"
	"github.com/mxhook/mxhook/router"
	"github.com/mxhook/mxhook/types"
)

func TestEventsReceivedAtIndexExists(t *testing.T) {
	store := openTestStore(t, filepath.Join(t.TempDir(), "mxhook.db"), bytes.Repeat([]byte{1}, 32), time.Second)
	t.Cleanup(func() { _ = store.Close() })
	var name string
	err := store.db.QueryRow(`SELECT name FROM sqlite_master
		WHERE type = 'index' AND tbl_name = 'mxhook_events' AND name = 'idx_mxhook_events_received_at'`).Scan(&name)
	if err != nil {
		t.Fatalf("received_at index missing: %v", err)
	}
}

type recordingRawDeleter struct {
	mu    sync.Mutex
	paths []string
}

func (r *recordingRawDeleter) Delete(_ context.Context, path string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.paths = append(r.paths, path)
	return nil
}

func TestJanitorSweepsExpiredTerminalState(t *testing.T) {
	store := openTestStore(t, filepath.Join(t.TempDir(), "mxhook.db"), bytes.Repeat([]byte{2}, 32), time.Second)
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()
	base := time.Now().UTC()
	store.now = func() time.Time { return base }

	rule := testRule()
	succeededEvent := testEvent()
	succeededEvent.ID, succeededEvent.TraceID = "event-succeeded", "trace-succeeded"
	succeededEvent.RawPath = "/raw/event-succeeded.eml"
	deadEvent := testEvent()
	deadEvent.ID, deadEvent.TraceID = "event-dead", "trace-dead"
	deadEvent.RawPath = "/raw/event-dead.eml"
	for _, event := range []*types.EmailEvent{succeededEvent, deadEvent} {
		event.ReceivedAt = base
		if err := store.Commit(ctx, event, []router.Rule{rule}); err != nil {
			t.Fatalf("Commit %s: %v", event.ID, err)
		}
	}
	for range 2 {
		delivery, err := store.ClaimDelivery(ctx)
		if err != nil {
			t.Fatalf("ClaimDelivery: %v", err)
		}
		switch delivery.MessageID {
		case succeededEvent.ID:
			if err := store.CompleteDelivery(ctx, delivery); err != nil {
				t.Fatalf("CompleteDelivery: %v", err)
			}
		case deadEvent.ID:
			if err := store.DeadDelivery(ctx, delivery, "permanent"); err != nil {
				t.Fatalf("DeadDelivery: %v", err)
			}
		default:
			t.Fatalf("unexpected delivery %q", delivery.MessageID)
		}
	}
	if err := store.RecordIngressFailure(ctx, queue.Message{ID: "poison", TraceID: "poison"}, "parse failure"); err != nil {
		t.Fatalf("RecordIngressFailure: %v", err)
	}

	store.now = func() time.Time { return base.Add(48 * time.Hour) }
	freshEvent := testEvent()
	freshEvent.ID, freshEvent.TraceID = "event-fresh", "trace-fresh"
	freshEvent.RawPath = "/raw/event-fresh.eml"
	freshEvent.ReceivedAt = base.Add(48 * time.Hour)
	if err := store.Commit(ctx, freshEvent, nil); err != nil {
		t.Fatalf("Commit fresh: %v", err)
	}

	raw := &recordingRawDeleter{}
	janitor, err := NewJanitor(store, raw, RetentionConfig{TTL: 24 * time.Hour})
	if err != nil {
		t.Fatalf("NewJanitor: %v", err)
	}
	stats, err := janitor.SweepOnce(ctx)
	if err != nil {
		t.Fatalf("SweepOnce: %v", err)
	}
	if stats.Deliveries != 1 || stats.Events != 1 || stats.IngressFailures != 1 || stats.RawFiles != 1 {
		t.Fatalf("stats = %+v, want 1/1/1/1", stats)
	}

	assertCount(t, store, "mxhook_deliveries", 1)
	assertCount(t, store, "mxhook_delivery_attempts", 1)
	assertCount(t, store, "mxhook_events", 2)
	assertCount(t, store, "mxhook_ingress_failures", 0)
	if len(raw.paths) != 1 || raw.paths[0] != succeededEvent.RawPath {
		t.Fatalf("raw deletions = %#v, want [%s]", raw.paths, succeededEvent.RawPath)
	}
	var state string
	if err := store.db.QueryRow(`SELECT state FROM mxhook_deliveries`).Scan(&state); err != nil {
		t.Fatalf("remaining delivery: %v", err)
	}
	if state != "dead" {
		t.Fatalf("remaining delivery state = %q, want dead", state)
	}
	var remaining int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM mxhook_events WHERE id IN ('event-dead', 'event-fresh')`).Scan(&remaining); err != nil {
		t.Fatalf("remaining events: %v", err)
	}
	if remaining != 2 {
		t.Fatalf("remaining events = %d, want dead + fresh retained", remaining)
	}
}

func TestNewJanitorValidatesConfig(t *testing.T) {
	store := openTestStore(t, filepath.Join(t.TempDir(), "mxhook.db"), bytes.Repeat([]byte{3}, 32), time.Second)
	t.Cleanup(func() { _ = store.Close() })
	if _, err := NewJanitor(store, nil, RetentionConfig{TTL: 0}); err == nil {
		t.Fatal("NewJanitor accepted zero TTL")
	}
	if _, err := NewJanitor(store, nil, RetentionConfig{TTL: time.Hour, Interval: -time.Second}); err == nil {
		t.Fatal("NewJanitor accepted negative interval")
	}
	if _, err := NewJanitor(nil, nil, RetentionConfig{TTL: time.Hour}); err == nil {
		t.Fatal("NewJanitor accepted nil store")
	}
}
