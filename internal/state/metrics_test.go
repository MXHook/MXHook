package state_test

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/mxhook/mxhook/internal/state"
	"github.com/mxhook/mxhook/queue"
	sqlitequeue "github.com/mxhook/mxhook/queue/sqlite"
	"github.com/mxhook/mxhook/router"
	"github.com/mxhook/mxhook/types"
)

func TestMetricsSnapshotOmitsQueueDepthWithoutQueueSource(t *testing.T) {
	// A split deployment gives the state store no access to the queue table;
	// the outbox and dead gauges must still be served rather than failing the
	// whole snapshot.
	store, err := state.Open(state.Config{
		Driver: "sqlite", DSN: filepath.Join(t.TempDir(), "mxhook.db"), EncryptionKey: bytes.Repeat([]byte{9}, 32),
		LeaseDuration: time.Second, PollInterval: time.Millisecond, RunMigrations: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	snapshot, err := store.MetricsSnapshot(context.Background())
	if err != nil {
		t.Fatalf("MetricsSnapshot without queue source: %v", err)
	}
	if snapshot.QueueDepth != 0 || snapshot.OutboxDepth != 0 || snapshot.DeadDepth != 0 {
		t.Fatalf("snapshot = %#v", snapshot)
	}
}

func TestMetricsSnapshotReadsQueueDepthFromQueueSource(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mxhook.db")
	queueStore, err := sqlitequeue.New(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = queueStore.Close() })
	message := queue.NewMessage("depth-message", "test", "", "sender@example.net",
		[]string{"inbox@example.com"}, time.Now(), []byte("Subject: depth\r\n\r\nbody"))
	if err := queueStore.Enqueue(context.Background(), message); err != nil {
		t.Fatal(err)
	}
	// Queue depth is owned by the queue adapter, not read out of its tables
	// by the state store.
	depth, err := queueStore.Depth(context.Background())
	if err != nil {
		t.Fatalf("Depth: %v", err)
	}
	if depth != 1 {
		t.Fatalf("queue depth = %d, want 1", depth)
	}
	store, err := state.Open(state.Config{
		Driver: "sqlite", DSN: path, EncryptionKey: bytes.Repeat([]byte{8}, 32),
		LeaseDuration: time.Second, PollInterval: time.Millisecond, RunMigrations: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	store.SetQueueDepthSource(queueStore)
	snapshot, err := store.MetricsSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.QueueDepth != 1 {
		t.Fatalf("snapshot = %#v, want queue depth 1 via the queue source", snapshot)
	}
}

func TestMetricsSnapshotReadsDurableBacklogs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mxhook.db")
	queueStore, err := sqlitequeue.New(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = queueStore.Close() })
	store, err := state.Open(state.Config{
		Driver: "sqlite", DSN: path, EncryptionKey: bytes.Repeat([]byte{8}, 32),
		LeaseDuration: time.Second, PollInterval: time.Millisecond, RunMigrations: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	store.SetQueueDepthSource(queueStore)
	message := queue.NewMessage("metrics-message", "test", "", "sender@example.net",
		[]string{"inbox@example.com"}, time.Now(), []byte("Subject: metrics\r\n\r\nbody"))
	if err := queueStore.Enqueue(context.Background(), message); err != nil {
		t.Fatal(err)
	}
	rule := router.Rule{
		ID: "metrics-route", RecipientPattern: "*", Target: router.TargetWebhook,
		WebhookURL: "https://example.com/hook", WebhookSecret: "secret",
	}
	event := &types.EmailEvent{
		SchemaVersion: types.EmailEventSchemaV1, ID: "metrics-event",
		TraceID: "trace", ReceivedAt: time.Now().UTC(), Headers: map[string][]string{},
	}
	if err := store.Commit(context.Background(), event, []router.Rule{rule}); err != nil {
		t.Fatal(err)
	}
	snapshot, err := store.MetricsSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.QueueDepth != 1 || snapshot.OutboxDepth != 1 || snapshot.DeadDepth != 0 {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	delivery, err := store.ClaimDelivery(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.DeadDelivery(context.Background(), delivery, "terminal"); err != nil {
		t.Fatal(err)
	}
	snapshot, err = store.MetricsSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.OutboxDepth != 0 || snapshot.DeadDepth != 1 {
		t.Fatalf("terminal snapshot = %#v", snapshot)
	}
}
