package sqlite

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mxhook/mxhook/queue"
)

var _ queue.Dequeuer = (*Adapter)(nil)

func TestAdapterRoundTripAndPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data", "mxhook.db")
	adapter, err := New(context.Background(), path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	msg := testQueueMessage("message-1", []byte("raw message"))
	if err := adapter.Enqueue(context.Background(), msg); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if err := adapter.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	adapter, err = New(context.Background(), path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = adapter.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	delivery, err := adapter.Claim(ctx)
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	got := delivery.Message
	if got.ID != msg.ID || !bytes.Equal(got.Body, msg.Body) {
		t.Fatalf("got = %#v, want %#v", got, msg)
	}
	if len(got.EnvelopeTo) != 1 || got.EnvelopeTo[0] != "inbox@example.com" {
		t.Errorf("recipients = %#v", got.EnvelopeTo)
	}
	if err := adapter.Ack(context.Background(), got.ID, delivery.Receipt); err != nil {
		t.Fatalf("Ack: %v", err)
	}
	var count int
	if err := adapter.db.QueryRow(`SELECT COUNT(*) FROM mxhook_queue WHERE id = ?`, got.ID).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Errorf("acked row count = %d, want 0", count)
	}
}

func TestSQLiteDatabaseAndDirectoryArePrivate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private", "mxhook.db")
	adapter, err := New(context.Background(), path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = adapter.Close() }()
	directory, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if got := directory.Mode().Perm(); got != 0o700 {
		t.Fatalf("database directory permissions = %o, want 700", got)
	}
	database, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := database.Mode().Perm(); got != 0o600 {
		t.Fatalf("database permissions = %o, want 600", got)
	}
}

func TestEnqueueIsIdempotentForIdenticalMessage(t *testing.T) {
	adapter, err := New(context.Background(), filepath.Join(t.TempDir(), "mxhook.db"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = adapter.Close() }()
	msg := testQueueMessage("same", []byte("body"))
	if err := adapter.Enqueue(context.Background(), msg); err != nil {
		t.Fatalf("first Enqueue: %v", err)
	}
	if err := adapter.Enqueue(context.Background(), msg); err != nil {
		t.Fatalf("duplicate Enqueue: %v", err)
	}
	var count int
	if err := adapter.db.QueryRow(`SELECT COUNT(*) FROM mxhook_queue WHERE id = ?`, msg.ID).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Errorf("count = %d, want 1", count)
	}
}

func TestEnqueueIsIdempotentAcrossSerializationChange(t *testing.T) {
	adapter, err := New(context.Background(), filepath.Join(t.TempDir(), "mxhook.db"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = adapter.Close() }()
	ctx := context.Background()
	msg := testQueueMessage("reserialized", []byte("body"))
	encoded, err := queue.EncodeMessage(msg)
	if err != nil {
		t.Fatalf("EncodeMessage: %v", err)
	}
	var reserialized bytes.Buffer
	if err := json.Indent(&reserialized, encoded, "", "  "); err != nil {
		t.Fatalf("Indent: %v", err)
	}
	if _, err := adapter.db.ExecContext(ctx,
		`INSERT INTO mxhook_queue (id, payload) VALUES (?, ?)`,
		msg.ID, reserialized.Bytes(),
	); err != nil {
		t.Fatalf("insert reserialized row: %v", err)
	}
	if err := adapter.Enqueue(ctx, msg); err != nil {
		t.Fatalf("Enqueue after serialization change: %v", err)
	}
	var count int
	if err := adapter.db.QueryRow(`SELECT COUNT(*) FROM mxhook_queue WHERE id = ?`, msg.ID).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Errorf("count = %d, want 1", count)
	}
}

func TestEnqueueRejectsIDCollision(t *testing.T) {
	adapter, err := New(context.Background(), filepath.Join(t.TempDir(), "mxhook.db"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = adapter.Close() }()
	ctx := context.Background()
	if err := adapter.Enqueue(ctx, testQueueMessage("collision", []byte("one"))); err != nil {
		t.Fatalf("first Enqueue: %v", err)
	}
	err = adapter.Enqueue(ctx, testQueueMessage("collision", []byte("two")))
	if !errors.Is(err, ErrIDCollision) {
		t.Fatalf("Enqueue error = %v, want ErrIDCollision", err)
	}
}

func TestAckDeletesRowAndStaysIdempotent(t *testing.T) {
	adapter, err := New(context.Background(), filepath.Join(t.TempDir(), "mxhook.db"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = adapter.Close() }()
	ctx := context.Background()
	if err := adapter.Enqueue(ctx, testQueueMessage("retained", []byte("body"))); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	delivery, err := adapter.Claim(ctx)
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if err := adapter.Ack(ctx, delivery.Message.ID, delivery.Receipt); err != nil {
		t.Fatalf("Ack: %v", err)
	}
	var count int
	if err := adapter.db.QueryRow(`SELECT COUNT(*) FROM mxhook_queue WHERE id = ?`, delivery.Message.ID).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Fatalf("acked row count = %d, want 0", count)
	}
	if err := adapter.Ack(ctx, delivery.Message.ID, delivery.Receipt); err != nil {
		t.Fatalf("repeated Ack after deletion: %v", err)
	}
}

func TestNackPersistsReason(t *testing.T) {
	adapter, err := New(context.Background(), filepath.Join(t.TempDir(), "mxhook.db"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = adapter.Close() }()
	ctx := context.Background()
	if err := adapter.Enqueue(ctx, testQueueMessage("failed", []byte("body"))); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	delivery, err := adapter.Claim(ctx)
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if err := adapter.Nack(ctx, "failed", delivery.Receipt, "parse failure"); err != nil {
		t.Fatalf("Nack: %v", err)
	}
	var state, reason string
	if err := adapter.db.QueryRow(`SELECT state, failure_reason FROM mxhook_queue WHERE id = ?`, "failed").Scan(&state, &reason); err != nil {
		t.Fatalf("query: %v", err)
	}
	if state != "nacked" || reason != "parse failure" {
		t.Errorf("state/reason = %q/%q", state, reason)
	}
}

func TestClaimQuarantinesUndecodablePayload(t *testing.T) {
	adapter, err := NewWithConfig(context.Background(), Config{
		Path:          filepath.Join(t.TempDir(), "mxhook.db"),
		PollInterval:  5 * time.Millisecond,
		RunMigrations: true,
	})
	if err != nil {
		t.Fatalf("NewWithConfig: %v", err)
	}
	defer func() { _ = adapter.Close() }()
	ctx := context.Background()
	if _, err := adapter.db.ExecContext(ctx,
		`INSERT INTO mxhook_queue (id, payload, created_at) VALUES (?, ?, ?)`,
		"poison", []byte("not a message"), "2026-08-27T00:00:00.000Z",
	); err != nil {
		t.Fatalf("insert poison row: %v", err)
	}
	msg := testQueueMessage("valid", []byte("body"))
	if err := adapter.Enqueue(ctx, msg); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	claimCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	delivery, err := adapter.Claim(claimCtx)
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if delivery.Message.ID != msg.ID {
		t.Fatalf("claimed message = %q, want %q", delivery.Message.ID, msg.ID)
	}

	var state, reason string
	if err := adapter.db.QueryRow(
		`SELECT state, COALESCE(failure_reason, '') FROM mxhook_queue WHERE id = ?`, "poison",
	).Scan(&state, &reason); err != nil {
		t.Fatalf("query poison row: %v", err)
	}
	if state != "nacked" {
		t.Errorf("poison state = %q, want nacked", state)
	}
	if reason == "" {
		t.Error("poison failure_reason is empty")
	}

	if err := adapter.Ack(ctx, delivery.Message.ID, delivery.Receipt); err != nil {
		t.Fatalf("Ack: %v", err)
	}
	emptyCtx, emptyCancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer emptyCancel()
	if _, err := adapter.Claim(emptyCtx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Claim after quarantine error = %v, want deadline", err)
	}
}

func TestMigrationsDisabledRequiresExistingSchema(t *testing.T) {
	adapter, err := NewWithConfig(context.Background(), Config{
		Path:          filepath.Join(t.TempDir(), "mxhook.db"),
		RunMigrations: false,
	})
	if err == nil {
		_ = adapter.Close()
		t.Fatal("NewWithConfig accepted a database without a queue schema")
	}
	if !strings.Contains(err.Error(), "schema") {
		t.Fatalf("error = %v, want schema verification failure", err)
	}
}

func TestMigrationsDisabledUsesExistingSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mxhook.db")
	initial, err := New(context.Background(), path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := initial.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	adapter, err := NewWithConfig(context.Background(), Config{Path: path, RunMigrations: false})
	if err != nil {
		t.Fatalf("NewWithConfig without migrations: %v", err)
	}
	defer func() { _ = adapter.Close() }()
	if err := adapter.Enqueue(context.Background(), testQueueMessage("gated", []byte("body"))); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
}

func TestSQLitePragmas(t *testing.T) {
	adapter, err := New(context.Background(), filepath.Join(t.TempDir(), "mxhook.db"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = adapter.Close() }()
	var journal string
	if err := adapter.db.QueryRow(`PRAGMA journal_mode`).Scan(&journal); err != nil {
		t.Fatalf("journal_mode: %v", err)
	}
	if journal != "wal" {
		t.Errorf("journal_mode = %q, want wal", journal)
	}
	var foreignKeys int
	if err := adapter.db.QueryRow(`PRAGMA foreign_keys`).Scan(&foreignKeys); err != nil {
		t.Fatalf("foreign_keys: %v", err)
	}
	if foreignKeys != 1 {
		t.Errorf("foreign_keys = %d, want 1", foreignKeys)
	}
}

func TestExpiredLeaseIsRedeliveredWithNewReceipt(t *testing.T) {
	adapter, err := NewWithConfig(context.Background(), Config{
		Path:          filepath.Join(t.TempDir(), "mxhook.db"),
		LeaseDuration: 200 * time.Millisecond,
		PollInterval:  time.Millisecond,
		RunMigrations: true,
	})
	if err != nil {
		t.Fatalf("NewWithConfig: %v", err)
	}
	defer func() { _ = adapter.Close() }()
	current := time.Date(2026, time.August, 28, 12, 0, 0, 0, time.UTC)
	adapter.now = func() time.Time { return current }
	ctx := context.Background()
	msg := testQueueMessage("crash-recovery", []byte("body"))
	if err := adapter.Enqueue(ctx, msg); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	first, err := adapter.Claim(ctx)
	if err != nil {
		t.Fatalf("first Receive: %v", err)
	}
	if first.Message.ID != msg.ID || first.Attempt != 1 || first.Receipt == "" {
		t.Fatalf("first delivery = %#v", first)
	}

	beforeExpiry, cancel := context.WithTimeout(ctx, 25*time.Millisecond)
	defer cancel()
	if _, err := adapter.Claim(beforeExpiry); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Receive before expiry error = %v, want deadline", err)
	}

	current = current.Add(250 * time.Millisecond)
	recoveryCtx, recoveryCancel := context.WithTimeout(ctx, time.Second)
	defer recoveryCancel()
	second, err := adapter.Claim(recoveryCtx)
	if err != nil {
		t.Fatalf("second Receive: %v", err)
	}
	if second.Message.ID != msg.ID || second.Attempt != 2 || second.Receipt == first.Receipt {
		t.Fatalf("redelivery = %#v; first = %#v", second, first)
	}
	if err := adapter.Ack(ctx, msg.ID, first.Receipt); !errors.Is(err, queue.ErrLeaseLost) {
		t.Fatalf("stale Ack error = %v, want ErrLeaseLost", err)
	}
	if err := adapter.Nack(ctx, msg.ID, first.Receipt, "stale"); !errors.Is(err, queue.ErrLeaseLost) {
		t.Fatalf("stale Nack error = %v, want ErrLeaseLost", err)
	}
	if _, err := adapter.Extend(ctx, msg.ID, first.Receipt, time.Second); !errors.Is(err, queue.ErrLeaseLost) {
		t.Fatalf("stale Extend error = %v, want ErrLeaseLost", err)
	}
	if err := adapter.Ack(ctx, msg.ID, second.Receipt); err != nil {
		t.Fatalf("Ack current receipt: %v", err)
	}
	if err := adapter.Ack(ctx, msg.ID, second.Receipt); err != nil {
		t.Fatalf("idempotent Ack: %v", err)
	}
}

func TestTwoAdaptersCannotHoldSameLiveLease(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mxhook.db")
	config := Config{Path: path, LeaseDuration: time.Second, PollInterval: 5 * time.Millisecond, RunMigrations: true}
	first, err := NewWithConfig(context.Background(), config)
	if err != nil {
		t.Fatalf("first NewWithConfig: %v", err)
	}
	defer func() { _ = first.Close() }()
	second, err := NewWithConfig(context.Background(), config)
	if err != nil {
		t.Fatalf("second NewWithConfig: %v", err)
	}
	defer func() { _ = second.Close() }()
	if err := first.Enqueue(context.Background(), testQueueMessage("single", []byte("body"))); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	type result struct {
		delivery queue.Delivery
		err      error
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	results := make(chan result, 2)
	go func() {
		delivery, err := first.Claim(ctx)
		results <- result{delivery: delivery, err: err}
	}()
	go func() {
		delivery, err := second.Claim(ctx)
		results <- result{delivery: delivery, err: err}
	}()

	successes := 0
	deadlines := 0
	for range 2 {
		got := <-results
		switch {
		case got.err == nil && got.delivery.Message.ID == "single":
			successes++
		case errors.Is(got.err, context.DeadlineExceeded):
			deadlines++
		default:
			t.Fatalf("unexpected claim result: %#v, %v", got.delivery, got.err)
		}
	}
	if successes != 1 || deadlines != 1 {
		t.Fatalf("successes/deadlines = %d/%d, want 1/1", successes, deadlines)
	}
}

func TestExtendPreventsPrematureRedelivery(t *testing.T) {
	adapter, err := NewWithConfig(context.Background(), Config{
		Path:          filepath.Join(t.TempDir(), "mxhook.db"),
		LeaseDuration: 100 * time.Millisecond,
		PollInterval:  time.Millisecond,
		RunMigrations: true,
	})
	if err != nil {
		t.Fatalf("NewWithConfig: %v", err)
	}
	defer func() { _ = adapter.Close() }()
	current := time.Date(2026, time.August, 28, 12, 0, 0, 0, time.UTC)
	adapter.now = func() time.Time { return current }
	ctx := context.Background()
	if err := adapter.Enqueue(ctx, testQueueMessage("extended", []byte("body"))); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	delivery, err := adapter.Claim(ctx)
	if err != nil {
		t.Fatalf("Receive: %v", err)
	}
	extendedUntil, err := adapter.Extend(ctx, delivery.Message.ID, delivery.Receipt, 300*time.Millisecond)
	if err != nil {
		t.Fatalf("Extend: %v", err)
	}
	if want := current.Add(300 * time.Millisecond); !extendedUntil.Equal(want) {
		t.Fatalf("extended deadline = %v, want %v", extendedUntil, want)
	}

	current = current.Add(150 * time.Millisecond)
	waitCtx, cancel := context.WithTimeout(ctx, 25*time.Millisecond)
	defer cancel()
	if _, err := adapter.Claim(waitCtx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Receive during extension error = %v, want deadline", err)
	}

	current = current.Add(200 * time.Millisecond)
	expiredCtx, expiredCancel := context.WithTimeout(ctx, time.Second)
	defer expiredCancel()
	redelivered, err := adapter.Claim(expiredCtx)
	if err != nil {
		t.Fatalf("Receive after extension expiry: %v", err)
	}
	if redelivered.Message.ID != delivery.Message.ID || redelivered.Attempt != 2 {
		t.Fatalf("redelivery = %#v", redelivered)
	}
}

func TestNackIsIdempotent(t *testing.T) {
	adapter, err := NewWithConfig(context.Background(), Config{Path: filepath.Join(t.TempDir(), "mxhook.db"), RunMigrations: true})
	if err != nil {
		t.Fatalf("NewWithConfig: %v", err)
	}
	defer func() { _ = adapter.Close() }()
	ctx := context.Background()
	if err := adapter.Enqueue(ctx, testQueueMessage("nack-lease", []byte("body"))); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	delivery, err := adapter.Claim(ctx)
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if err := adapter.Nack(ctx, delivery.Message.ID, delivery.Receipt, "permanent"); err != nil {
		t.Fatalf("Nack: %v", err)
	}
	if err := adapter.Nack(ctx, delivery.Message.ID, delivery.Receipt, "permanent"); err != nil {
		t.Fatalf("idempotent Nack: %v", err)
	}
	if state := messageState(t, adapter, delivery.Message.ID); state != "nacked" {
		t.Fatalf("state = %q, want nacked", state)
	}
}

func messageState(t *testing.T, adapter *Adapter, id string) string {
	t.Helper()
	var state string
	if err := adapter.db.QueryRow(`SELECT state FROM mxhook_queue WHERE id = ?`, id).Scan(&state); err != nil {
		t.Fatalf("message state: %v", err)
	}
	return state
}

func testQueueMessage(id string, body []byte) queue.Message {
	return queue.NewMessage(
		id,
		"test",
		"",
		"sender@example.com",
		[]string{"inbox@example.com"},
		time.Date(2026, time.August, 28, 12, 0, 0, 0, time.UTC),
		body,
	)
}
