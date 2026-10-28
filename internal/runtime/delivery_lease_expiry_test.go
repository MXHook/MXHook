package runtime

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/mxhook/mxhook/internal/state"
	"github.com/mxhook/mxhook/router"
	"github.com/mxhook/mxhook/types"
	"github.com/mxhook/mxhook/webhook"
)

// stalledClaimStore delays each claim's return by stall, modelling the gap
// between minting a lease and actually using it: scheduler pressure, a GC
// pause, or a claim transaction that commits slower than it computed its own
// deadline. The store stamps locked_until at the start of the claim, so a long
// enough gap hands the worker a lease that is already dead on arrival -- the
// "lockedUntil=-111ms" shape seen in production.
type stalledClaimStore struct {
	DeliveryStore
	stall time.Duration
}

func (s stalledClaimStore) ClaimDelivery(ctx context.Context) (state.Delivery, error) {
	delivery, err := s.DeliveryStore.ClaimDelivery(ctx)
	if err != nil {
		return delivery, fmt.Errorf("stalled claim: %w", err)
	}
	select {
	case <-time.After(s.stall):
	case <-ctx.Done():
		return state.Delivery{}, ctx.Err()
	}
	return delivery, nil
}

// TestDeliveryClaimedWithDeadLeaseIsNotRedelivered pins the rule that a webhook
// actually accepted by the endpoint is never posted again merely because its
// lease had already elapsed.
//
// With a lease that is dead on arrival, every renewal and every completion
// write is fenced out by locked_until > now. The row stays 'processing' with a
// dead lease, matches the reclaim predicate state='processing' AND
// locked_until <= now, and is delivered again -- and again, without bound,
// because the attempt budget is never durably recorded either.
func TestDeliveryClaimedWithDeadLeaseIsNotRedelivered(t *testing.T) {
	const lease = 50 * time.Millisecond

	var mu sync.Mutex
	var hits []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		mu.Lock()
		hits = append(hits, request.Header.Get("X-MXHook-Delivery-ID"))
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	hitCount := func() int {
		mu.Lock()
		defer mu.Unlock()
		return len(hits)
	}

	store, err := state.Open(state.Config{
		Driver: "sqlite", DSN: filepath.Join(t.TempDir(), "mxhook.db"),
		EncryptionKey: bytes.Repeat([]byte{11}, 32),
		LeaseDuration: lease, PollInterval: 2 * time.Millisecond, RunMigrations: true,
	})
	if err != nil {
		t.Fatalf("state.Open: %v", err)
	}
	defer func() { _ = store.Close() }()

	rule := router.Rule{
		ID: "slow-route", RecipientPattern: "*", Target: router.TargetWebhook,
		WebhookURL: server.URL, WebhookSecret: "slow-secret",
	}
	event := &types.EmailEvent{
		SchemaVersion: types.EmailEventSchemaV1, ID: "slow-message", TraceID: "trace",
		ReceivedAt: time.Now().UTC(), EnvelopeTo: []string{"inbox@example.com"},
		Headers: map[string][]string{},
	}
	if err := store.Commit(context.Background(), event, []router.Rule{rule}); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	// LeaseExtension matches the store's lease so renewals cannot mask the race
	// by silently buying a longer window than production would grant.
	worker, err := NewDeliveryWorker(
		stalledClaimStore{DeliveryStore: store, stall: 2 * lease},
		webhook.NewDeliverer(webhook.Config{MaxAttempts: 1, TimeoutSecs: 5, AllowHTTP: true, AllowPrivateNetwork: true}),
		DeliveryWorkerConfig{MaxAttempts: 3, LeaseExtension: lease, BaseRetry: 5 * time.Millisecond, MaxRetry: 10 * time.Millisecond},
	)
	if err != nil {
		t.Fatalf("NewDeliveryWorker: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- worker.Run(ctx) }()

	deadline := time.Now().Add(10 * time.Second)
	for hitCount() == 0 {
		if time.Now().After(deadline) {
			cancel()
			t.Fatal("webhook was never called")
		}
		time.Sleep(2 * time.Millisecond)
	}
	mu.Lock()
	deliveryID := hits[0]
	mu.Unlock()

	// Wait for the durable terminal state. A row whose completion keeps losing
	// to lease expiry never reaches one -- it is re-claimed and re-posted
	// forever -- so this loop is also the liveness assertion.
	var record state.DeliveryRecord
	for {
		var getErr error
		record, getErr = store.GetDelivery(context.Background(), deliveryID)
		if getErr == nil && (record.State == "succeeded" || record.State == "dead") {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatalf("delivery never reached a terminal state: record=%#v err=%v hits=%d",
				record, getErr, hitCount())
		}
		time.Sleep(2 * time.Millisecond)
	}
	if record.State != "succeeded" {
		cancel()
		t.Fatalf("state = %q, want succeeded: the endpoint accepted the webhook", record.State)
	}

	// Settle well past several claim-plus-stall cycles so a late duplicate post
	// cannot slip in after the assertion.
	time.Sleep(6 * lease)
	if got := hitCount(); got != 1 {
		mu.Lock()
		observed := append([]string(nil), hits...)
		mu.Unlock()
		cancel()
		t.Fatalf("webhook hits = %d, want exactly 1: a successful delivery was redelivered after its lease expired (hits=%v)",
			got, observed)
	}

	cancel()
	if err := <-done; err != nil {
		t.Fatalf("DeliveryWorker.Run: %v", err)
	}
}

// TestStaleReceiptCannotCompleteRowOwnedByAnotherWorker guards the fence that
// the redelivery fix must not weaken: once a second worker has claimed a row,
// the first worker's receipt is dead and can neither complete nor retry it, no
// matter how the completion write is scoped.
func TestStaleReceiptCannotCompleteRowOwnedByAnotherWorker(t *testing.T) {
	store, err := state.Open(state.Config{
		Driver: "sqlite", DSN: filepath.Join(t.TempDir(), "mxhook.db"),
		EncryptionKey: bytes.Repeat([]byte{12}, 32),
		LeaseDuration: 20 * time.Millisecond, PollInterval: 2 * time.Millisecond, RunMigrations: true,
	})
	if err != nil {
		t.Fatalf("state.Open: %v", err)
	}
	defer func() { _ = store.Close() }()

	rule := router.Rule{
		ID: "fence-route", RecipientPattern: "*", Target: router.TargetWebhook,
		WebhookURL: "http://127.0.0.1:1/hook", WebhookSecret: "fence-secret",
	}
	event := &types.EmailEvent{
		SchemaVersion: types.EmailEventSchemaV1, ID: "fence-message", TraceID: "trace",
		ReceivedAt: time.Now().UTC(), EnvelopeTo: []string{"inbox@example.com"},
		Headers: map[string][]string{},
	}
	if err := store.Commit(context.Background(), event, []router.Rule{rule}); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	first, err := store.ClaimDelivery(context.Background())
	if err != nil {
		t.Fatalf("first ClaimDelivery: %v", err)
	}
	// Let the first lease lapse so the row becomes reclaimable, then take it.
	time.Sleep(40 * time.Millisecond)
	second, err := store.ClaimDelivery(context.Background())
	if err != nil {
		t.Fatalf("second ClaimDelivery: %v", err)
	}
	if first.Receipt == second.Receipt {
		t.Fatalf("second claim reused receipt %q", second.Receipt)
	}

	// The first worker is now stale: another worker owns the row. Every finish
	// path must refuse its receipt.
	if err := store.CompleteDelivery(context.Background(), first); !errors.Is(err, state.ErrLeaseLost) {
		t.Fatalf("stale CompleteDelivery = %v, want ErrLeaseLost", err)
	}
	if err := store.RetryDelivery(context.Background(), first, "stale", time.Now()); !errors.Is(err, state.ErrLeaseLost) {
		t.Fatalf("stale RetryDelivery = %v, want ErrLeaseLost", err)
	}
	if err := store.DeadDelivery(context.Background(), first, "stale"); !errors.Is(err, state.ErrLeaseLost) {
		t.Fatalf("stale DeadDelivery = %v, want ErrLeaseLost", err)
	}
	if _, err := store.ExtendDelivery(context.Background(), first, time.Second); !errors.Is(err, state.ErrLeaseLost) {
		t.Fatalf("stale ExtendDelivery = %v, want ErrLeaseLost", err)
	}

	// The rightful owner still completes normally.
	if err := store.CompleteDelivery(context.Background(), second); err != nil {
		t.Fatalf("owner CompleteDelivery: %v", err)
	}
	record, err := store.GetDelivery(context.Background(), second.ID)
	if err != nil {
		t.Fatalf("GetDelivery: %v", err)
	}
	if record.State != "succeeded" {
		t.Fatalf("state = %q, want succeeded", record.State)
	}
}
