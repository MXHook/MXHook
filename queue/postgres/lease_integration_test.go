package postgres

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mxhook/mxhook/queue"
)

var integrationSchemaSequence atomic.Uint64

func newIntegrationDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse TEST_DATABASE_URL: %v", err)
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect to integration database: %v", err)
	}
	schema := fmt.Sprintf("mxhook_test_%d_%d", time.Now().UnixNano(), integrationSchemaSequence.Add(1))
	quotedSchema := pgx.Identifier{schema}.Sanitize()
	if _, err := admin.Exec(ctx, `CREATE SCHEMA `+quotedSchema); err != nil {
		admin.Close()
		t.Fatalf("create integration schema: %v", err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec(context.Background(), `DROP SCHEMA `+quotedSchema+` CASCADE`); err != nil {
			t.Errorf("drop integration schema: %v", err)
		}
		admin.Close()
	})
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func TestLeaseRecoveryIntegration(t *testing.T) {
	dsn := newIntegrationDSN(t)
	ctx := context.Background()
	adapter, err := NewWithConfig(ctx, Config{
		DSN:           dsn,
		LeaseDuration: 200 * time.Millisecond,
		PollInterval:  5 * time.Millisecond,
		RunMigrations: true,
	})
	if err != nil {
		t.Fatalf("NewWithConfig: %v", err)
	}
	defer adapter.Close()
	msg := integrationMessage("lease-recovery", []byte("body"))
	if err := adapter.Enqueue(ctx, msg); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	first, err := adapter.Claim(ctx)
	if err != nil {
		t.Fatalf("first Claim: %v", err)
	}
	if first.Attempt != 1 || first.Receipt == "" {
		t.Fatalf("first delivery = %#v", first)
	}
	time.Sleep(225 * time.Millisecond)
	// The short lease above exists only to drive the expiry that makes the row
	// reclaimable. The reclaim itself, and every acknowledgement assertion after
	// it, is about lease ownership rather than about finishing inside 200ms, so
	// take the recovered lease through a long-leased adapter. Nothing below then
	// runs against a clock: there is no window left to race.
	recoveryAdapter, err := NewWithConfig(ctx, Config{
		DSN:           dsn,
		LeaseDuration: time.Minute,
		PollInterval:  5 * time.Millisecond,
		RunMigrations: true,
	})
	if err != nil {
		t.Fatalf("recovery NewWithConfig: %v", err)
	}
	defer recoveryAdapter.Close()
	recoveryCtx, recoveryCancel := context.WithTimeout(ctx, 30*time.Second)
	defer recoveryCancel()
	second, err := recoveryAdapter.Claim(recoveryCtx)
	if err != nil {
		t.Fatalf("second Claim: %v", err)
	}
	if second.Attempt != 2 || second.Receipt == first.Receipt {
		t.Fatalf("second delivery = %#v", second)
	}
	// The first receipt lost its lease to expiry, so every operation carrying it
	// must be rejected. This is the real point of the block and is unchanged.
	if err := adapter.Ack(ctx, msg.ID, first.Receipt); !errors.Is(err, queue.ErrLeaseLost) {
		t.Fatalf("stale Ack error = %v, want ErrLeaseLost", err)
	}
	if err := adapter.Nack(ctx, msg.ID, first.Receipt, "stale"); !errors.Is(err, queue.ErrLeaseLost) {
		t.Fatalf("stale Nack error = %v, want ErrLeaseLost", err)
	}
	if _, err := adapter.Extend(ctx, msg.ID, first.Receipt, time.Second); !errors.Is(err, queue.ErrLeaseLost) {
		t.Fatalf("stale Extend error = %v, want ErrLeaseLost", err)
	}
	if err := recoveryAdapter.Ack(ctx, msg.ID, second.Receipt); err != nil {
		t.Fatalf("Ack: %v", err)
	}
	if err := recoveryAdapter.Ack(ctx, msg.ID, second.Receipt); err != nil {
		t.Fatalf("idempotent Ack: %v", err)
	}

	nacked := integrationMessage("lease-nack", []byte("body"))
	if err := adapter.Enqueue(ctx, nacked); err != nil {
		t.Fatalf("enqueue nack message: %v", err)
	}
	// Claimed through the long-leased adapter for the same reason: these
	// assertions are about nack semantics and idempotence, not about racing a
	// 200ms budget.
	nackDelivery, err := recoveryAdapter.Claim(ctx)
	if err != nil {
		t.Fatalf("claim nack message: %v", err)
	}
	if err := recoveryAdapter.Nack(ctx, nacked.ID, nackDelivery.Receipt, "permanent"); err != nil {
		t.Fatalf("Nack: %v", err)
	}
	if err := recoveryAdapter.Nack(ctx, nacked.ID, nackDelivery.Receipt, "permanent"); err != nil {
		t.Fatalf("idempotent Nack: %v", err)
	}
	var nackState, nackReason string
	if err := adapter.pool.QueryRow(ctx,
		`SELECT state, COALESCE(failure_reason, '') FROM mxhook_queue WHERE id = $1`, nacked.ID,
	).Scan(&nackState, &nackReason); err != nil {
		t.Fatalf("query nacked row: %v", err)
	}
	if nackState != "nacked" || nackReason != "permanent" {
		t.Errorf("nacked state/reason = %q/%q, want nacked/permanent", nackState, nackReason)
	}

	if err := adapter.Enqueue(ctx, integrationMessage("lease-concurrency", []byte("body"))); err != nil {
		t.Fatalf("enqueue concurrency message: %v", err)
	}
	// Both racers hold long leases so the contested row cannot legitimately be
	// redelivered by lease expiry while the race is being decided; any second
	// claim of the same row is therefore a mutual-exclusion break, not recovery.
	firstRacer, err := NewWithConfig(ctx, Config{
		DSN:           dsn,
		LeaseDuration: time.Minute,
		PollInterval:  5 * time.Millisecond,
		RunMigrations: true,
	})
	if err != nil {
		t.Fatalf("first racer NewWithConfig: %v", err)
	}
	defer firstRacer.Close()
	secondRacer, err := NewWithConfig(ctx, Config{
		DSN:           dsn,
		LeaseDuration: time.Minute,
		PollInterval:  5 * time.Millisecond,
		RunMigrations: true,
	})
	if err != nil {
		t.Fatalf("second racer NewWithConfig: %v", err)
	}
	defer secondRacer.Close()
	type claimResult struct {
		delivery queue.Delivery
		err      error
	}
	// Both racers keep polling for the whole settle window rather than stopping
	// at the first result, so the loser really does hammer the contested row
	// while the winner holds it. The window is a liveness bound, not a
	// correctness one: it is long enough that a slow round-trip still completes,
	// and far shorter than the minute-long leases above, so a second delivery
	// can never be attributed to legitimate expiry recovery.
	claimCtx, claimCancel := context.WithTimeout(ctx, 2*time.Second)
	defer claimCancel()
	results := make(chan claimResult, 2)
	var started sync.WaitGroup
	started.Add(2)
	for _, racer := range []*Adapter{firstRacer, secondRacer} {
		go func() {
			started.Done()
			started.Wait()
			delivery, err := racer.Claim(claimCtx)
			results <- claimResult{delivery: delivery, err: err}
		}()
	}
	var claimed []queue.Delivery
	for range 2 {
		got := <-results
		switch {
		case got.err == nil:
			claimed = append(claimed, got.delivery)
		case errors.Is(got.err, context.Canceled), errors.Is(got.err, context.DeadlineExceeded):
			// Polled the whole window without ever obtaining the row: this is
			// exactly what mutual exclusion is supposed to produce for the loser.
		default:
			t.Fatalf("unexpected concurrent claim: %#v, %v", got.delivery, got.err)
		}
	}
	// Mutual exclusion: the contested row may be handed out at most once. Two
	// deliveries of "lease-concurrency" means FOR UPDATE SKIP LOCKED stopped
	// serialising the claim, regardless of which goroutine was quicker.
	contested := 0
	var contestedReceipts []string
	for _, delivery := range claimed {
		if delivery.Message.ID != "lease-concurrency" {
			t.Fatalf("claimed unexpected message %q during contention", delivery.Message.ID)
		}
		contested++
		contestedReceipts = append(contestedReceipts, delivery.Receipt)
		if delivery.Receipt == "" {
			t.Fatal("contested claim returned an empty receipt")
		}
	}
	if contested != 1 {
		t.Fatalf("contested claims = %d (receipts %q), want exactly 1", contested, contestedReceipts)
	}
	// A single row must also never yield two distinct leases. Asserting the
	// receipts explicitly makes a duplicated hand-off loud rather than implicit
	// in the count above.
	if len(contestedReceipts) == 2 && contestedReceipts[0] != contestedReceipts[1] {
		t.Fatalf("two distinct receipts issued for one row: %q and %q",
			contestedReceipts[0], contestedReceipts[1])
	}
}

func TestExtendPreventsRedeliveryIntegration(t *testing.T) {
	dsn := newIntegrationDSN(t)
	ctx := context.Background()
	adapter, err := NewWithConfig(ctx, Config{
		DSN:           dsn,
		LeaseDuration: 200 * time.Millisecond,
		PollInterval:  5 * time.Millisecond,
		RunMigrations: true,
	})
	if err != nil {
		t.Fatalf("NewWithConfig: %v", err)
	}
	defer adapter.Close()
	msg := integrationMessage("lease-extend", []byte("body"))
	if err := adapter.Enqueue(ctx, msg); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	delivery, err := adapter.Claim(ctx)
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}

	before := time.Now()
	extended, err := adapter.Extend(ctx, msg.ID, delivery.Receipt, 5*time.Second)
	if err != nil {
		t.Fatalf("Extend: %v", err)
	}
	after := time.Now()
	// The deadline is derived from the Go clock, not the database clock, so
	// renewal scheduling stays correct under clock skew between the two.
	if extended.Before(before.Add(5*time.Second)) || extended.After(after.Add(5*time.Second)) {
		t.Fatalf("extended deadline %v outside Go-clock window [%v, %v]",
			extended, before.Add(5*time.Second), after.Add(5*time.Second))
	}
	if !extended.After(delivery.LockedUntil) {
		t.Fatalf("extended deadline %v does not advance original %v", extended, delivery.LockedUntil)
	}

	// The original lease duration has elapsed, but the extension holds.
	time.Sleep(250 * time.Millisecond)
	waitCtx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	if _, err := adapter.Claim(waitCtx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Claim during extension error = %v, want deadline", err)
	}
	if err := adapter.Ack(ctx, msg.ID, delivery.Receipt); err != nil {
		t.Fatalf("Ack after extension: %v", err)
	}
}

func TestClaimReturnsGoClockDeadlineIntegration(t *testing.T) {
	dsn := newIntegrationDSN(t)
	ctx := context.Background()
	adapter, err := NewWithConfig(ctx, Config{
		DSN:           dsn,
		LeaseDuration: 30 * time.Second,
		PollInterval:  5 * time.Millisecond,
		RunMigrations: true,
	})
	if err != nil {
		t.Fatalf("NewWithConfig: %v", err)
	}
	defer adapter.Close()
	msg := integrationMessage("lease-deadline", []byte("body"))
	if err := adapter.Enqueue(ctx, msg); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	before := time.Now()
	delivery, err := adapter.Claim(ctx)
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	after := time.Now()
	if delivery.LockedUntil.Before(before.Add(30*time.Second)) || delivery.LockedUntil.After(after.Add(30*time.Second)) {
		t.Fatalf("claim deadline %v outside Go-clock window [%v, %v]",
			delivery.LockedUntil, before.Add(30*time.Second), after.Add(30*time.Second))
	}
}

func TestClaimQuarantinesUndecodablePayloadIntegration(t *testing.T) {
	dsn := newIntegrationDSN(t)
	ctx := context.Background()
	adapter, err := NewWithConfig(ctx, Config{
		DSN:           dsn,
		PollInterval:  5 * time.Millisecond,
		RunMigrations: true,
	})
	if err != nil {
		t.Fatalf("NewWithConfig: %v", err)
	}
	defer adapter.Close()
	if _, err := adapter.pool.Exec(ctx,
		`INSERT INTO mxhook_queue (id, payload, created_at)
		 VALUES ($1, $2::jsonb, NOW() - INTERVAL '1 hour')`,
		"poison", []byte(`{"schema_version":"bogus"}`),
	); err != nil {
		t.Fatalf("insert poison row: %v", err)
	}
	msg := integrationMessage("valid", []byte("body"))
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
	if err := adapter.pool.QueryRow(ctx,
		`SELECT state, COALESCE(failure_reason, '') FROM mxhook_queue WHERE id = $1`, "poison",
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

func TestAckDeletesRowIntegration(t *testing.T) {
	dsn := newIntegrationDSN(t)
	ctx := context.Background()
	adapter, err := New(ctx, dsn)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer adapter.Close()
	if err := adapter.Enqueue(ctx, integrationMessage("ack-delete", []byte("body"))); err != nil {
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
	if err := adapter.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM mxhook_queue WHERE id = $1`, delivery.Message.ID,
	).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Fatalf("acked row count = %d, want 0", count)
	}
	if err := adapter.Ack(ctx, delivery.Message.ID, delivery.Receipt); err != nil {
		t.Fatalf("repeated Ack after deletion: %v", err)
	}
}

func TestIntegrationSchemasAreIsolated(t *testing.T) {
	ctx := context.Background()
	first, err := New(ctx, newIntegrationDSN(t))
	if err != nil {
		t.Fatalf("first New: %v", err)
	}
	defer first.Close()
	second, err := New(ctx, newIntegrationDSN(t))
	if err != nil {
		t.Fatalf("second New: %v", err)
	}
	defer second.Close()

	id := "shared-id"
	if err := first.Enqueue(ctx, integrationMessage(id, []byte("first"))); err != nil {
		t.Fatalf("first Enqueue: %v", err)
	}
	if err := second.Enqueue(ctx, integrationMessage(id, []byte("second"))); err != nil {
		t.Fatalf("second Enqueue: %v", err)
	}
}

func integrationMessage(id string, body []byte) queue.Message {
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
