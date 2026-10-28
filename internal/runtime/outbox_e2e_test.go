package runtime

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/mxhook/mxhook/internal/state"
	"github.com/mxhook/mxhook/parser"
	"github.com/mxhook/mxhook/queue"
	sqlitequeue "github.com/mxhook/mxhook/queue/sqlite"
	"github.com/mxhook/mxhook/router"
	"github.com/mxhook/mxhook/storage"
	"github.com/mxhook/mxhook/types"
	"github.com/mxhook/mxhook/webhook"
)

func TestSQLiteIngressOutboxDeliverySurvivesIndependentWorkers(t *testing.T) {
	// Record every hit rather than handing off through a size-1 channel: a
	// blocked handler would stall the worker holding the lease and cascade into
	// further redelivery, hiding the original signal.
	var mu sync.Mutex
	var hits []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		payload, _ := io.ReadAll(request.Body)
		if !webhook.VerifyTimestampedSignature(payload, "outbox-secret",
			request.Header.Get("X-MXHook-Signature"), request.Header.Get("X-MXHook-Timestamp"), 0) {
			t.Errorf("invalid webhook signature")
		}
		deliveryID := request.Header.Get("X-MXHook-Delivery-ID")
		if deliveryID == "" {
			t.Errorf("missing stable delivery ID")
		}
		mu.Lock()
		hits = append(hits, deliveryID)
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	hitCount := func() int {
		mu.Lock()
		defer mu.Unlock()
		return len(hits)
	}

	// Leases must outlive a healthy in-process unit of work. Leases short enough
	// to expire mid-flight let the fenced completion write lose to expiry, and
	// the row is re-claimed and re-delivered.
	databasePath := filepath.Join(t.TempDir(), "mxhook.db")
	queueStore, err := sqlitequeue.NewWithConfig(context.Background(), sqlitequeue.Config{
		Path: databasePath, LeaseDuration: 10 * time.Second, PollInterval: 5 * time.Millisecond,
		RunMigrations: true,
	})
	if err != nil {
		t.Fatalf("open queue: %v", err)
	}
	defer func() { _ = queueStore.Close() }()
	stateStore, err := state.Open(state.Config{
		Driver: "sqlite", DSN: databasePath, EncryptionKey: bytes.Repeat([]byte{4}, 32),
		LeaseDuration: 10 * time.Second, PollInterval: 5 * time.Millisecond, RunMigrations: true,
	})
	if err != nil {
		t.Fatalf("open state: %v", err)
	}
	defer func() { _ = stateStore.Close() }()
	rule := router.Rule{
		ID: "outbox-route", Domain: "example.com", RecipientPattern: "*@example.com",
		Target: router.TargetWebhook, WebhookURL: server.URL, WebhookSecret: "outbox-secret",
	}
	if err := stateStore.EnsureRoute(context.Background(), rule); err != nil {
		t.Fatalf("EnsureRoute: %v", err)
	}
	intake, err := NewIntakeProcessor(
		parser.New(), storage.NewFilesystemStorer(filepath.Join(t.TempDir(), "raw")), stateStore, stateStore,
	)
	if err != nil {
		t.Fatalf("NewIntakeProcessor: %v", err)
	}
	ingressWorker, err := NewWorker(queueStore, intake, WorkerConfig{LeaseExtension: 10 * time.Second})
	if err != nil {
		t.Fatalf("NewWorker: %v", err)
	}
	deliveryWorker, err := NewDeliveryWorker(
		stateStore,
		webhook.NewDeliverer(webhook.Config{MaxAttempts: 1, TimeoutSecs: 1, AllowHTTP: true, AllowPrivateNetwork: true}),
		DeliveryWorkerConfig{MaxAttempts: 3, LeaseExtension: 10 * time.Second, BaseRetry: 5 * time.Millisecond, MaxRetry: 20 * time.Millisecond},
	)
	if err != nil {
		t.Fatalf("NewDeliveryWorker: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 2)
	go func() { done <- ingressWorker.Run(ctx) }()
	go func() { done <- deliveryWorker.Run(ctx) }()
	message := queue.NewMessage(
		"outbox-message", "test", "", "sender@example.net", []string{"inbox@example.com"}, time.Now(),
		[]byte("Subject: durable outbox\r\nMessage-ID: <outbox@example.net>\r\n\r\nbody"),
	)
	if err := queueStore.Enqueue(ctx, message); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	deadline := time.Now().Add(10 * time.Second)
	for hitCount() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("webhook was not delivered")
		}
		time.Sleep(5 * time.Millisecond)
	}
	mu.Lock()
	deliveryID := hits[0]
	mu.Unlock()

	// Synchronize on the durable terminal state, then assert exactly what the
	// loop waited for -- the exit condition and the assertions below agree.
	var record state.DeliveryRecord
	for {
		var getErr error
		record, getErr = stateStore.GetDelivery(context.Background(), deliveryID)
		if getErr == nil && record.State == "succeeded" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("delivery did not reach succeeded state: record=%#v err=%v", record, getErr)
		}
		time.Sleep(5 * time.Millisecond)
	}
	if record.AttemptCount != 1 {
		t.Fatalf("attempt count = %d, want 1: a healthy delivery must not be retried", record.AttemptCount)
	}
	if got := hitCount(); got != 1 {
		t.Fatalf("webhook hits = %d, want exactly 1", got)
	}

	// The webhook fires from the delivery worker, which runs independently of
	// the ingress worker's ack; poll for the terminal queue state rather than
	// assuming the ack has already landed. Asserting the stored state (not
	// claimability) keeps this independent of lease expiry timing.
	// Ack removes the row under the queue retention policy, so an absent row is
	// the terminal acked state.
	ackDeadline := time.Now().Add(10 * time.Second)
	var queueState string
	for {
		queueState, err = queueStore.State(context.Background(), message.ID)
		if errors.Is(err, sql.ErrNoRows) {
			break
		}
		if err != nil {
			t.Fatalf("queue State: %v", err)
		}
		if time.Now().After(ackDeadline) {
			t.Fatalf("ingress message state = %q, want row removed after outbox commit", queueState)
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	for range 2 {
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("worker stopped with error: %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("worker did not stop")
		}
	}
}

func TestDurableDeliveryWorkerRetriesTemporaryFailure(t *testing.T) {
	// Every request is recorded before the response is written, so the log is a
	// faithful record of real HTTP hits: the first is rejected as temporary, the
	// rest succeed. Recording under a mutex (not a buffered channel) keeps a
	// duplicate delivery from deadlocking the handler and stalling the worker.
	var mu sync.Mutex
	var hits []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		mu.Lock()
		hits = append(hits, request.Header.Get("X-MXHook-Delivery-ID"))
		first := len(hits) == 1
		mu.Unlock()
		if first {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	hitCount := func() int {
		mu.Lock()
		defer mu.Unlock()
		return len(hits)
	}
	// The lease must outlive a healthy in-process delivery. A lease short enough
	// to expire mid-flight lets the fenced completion write lose to expiry, and
	// the row is re-claimed and re-delivered -- redelivery this test is not
	// trying to measure.
	store, err := state.Open(state.Config{
		Driver: "sqlite", DSN: filepath.Join(t.TempDir(), "mxhook.db"), EncryptionKey: bytes.Repeat([]byte{6}, 32),
		LeaseDuration: 10 * time.Second, PollInterval: 2 * time.Millisecond, RunMigrations: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	rule := router.Rule{
		ID: "retry-route", RecipientPattern: "*", Target: router.TargetWebhook,
		WebhookURL: server.URL, WebhookSecret: "retry-secret",
	}
	event := &types.EmailEvent{
		SchemaVersion: types.EmailEventSchemaV1, ID: "retry-message", TraceID: "trace",
		ReceivedAt: time.Now().UTC(), EnvelopeTo: []string{"inbox@example.com"}, Headers: map[string][]string{},
	}
	if err := store.Commit(context.Background(), event, []router.Rule{rule}); err != nil {
		t.Fatal(err)
	}
	worker, err := NewDeliveryWorker(store,
		webhook.NewDeliverer(webhook.Config{MaxAttempts: 1, TimeoutSecs: 1, AllowHTTP: true, AllowPrivateNetwork: true}),
		DeliveryWorkerConfig{MaxAttempts: 3, LeaseExtension: 10 * time.Second, BaseRetry: 5 * time.Millisecond, MaxRetry: 10 * time.Millisecond},
	)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- worker.Run(ctx) }()

	// The delivery ID is stable across attempts, so the first hit names the row
	// to watch. Waiting for a hit before reading the store also guarantees the
	// row exists.
	deadline := time.Now().Add(10 * time.Second)
	for hitCount() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("webhook was never called")
		}
		time.Sleep(2 * time.Millisecond)
	}
	mu.Lock()
	deliveryID := hits[0]
	mu.Unlock()
	if deliveryID == "" {
		t.Fatal("missing stable delivery ID header")
	}

	// Synchronize on the durable terminal state, then assert exactly what the
	// loop waited for. The stored record is the authority: the worker writes it
	// after the delivery it describes, so a record that has reached "succeeded"
	// with two attempts means both HTTP hits already happened.
	var record state.DeliveryRecord
	for {
		var getErr error
		record, getErr = store.GetDelivery(context.Background(), deliveryID)
		if getErr == nil && record.State == "succeeded" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("delivery did not succeed: record=%#v err=%v hits=%d", record, getErr, hitCount())
		}
		time.Sleep(2 * time.Millisecond)
	}
	if record.AttemptCount != 2 {
		t.Fatalf("attempt count = %d, want 2 (one rejected, one accepted): record=%#v", record.AttemptCount, record)
	}
	if got := hitCount(); got != 2 {
		t.Fatalf("webhook hits = %d, want 2: the endpoint must be retried exactly once after the 503", got)
	}
	mu.Lock()
	observed := append([]string(nil), hits...)
	mu.Unlock()
	for i, id := range observed {
		if id != deliveryID {
			t.Fatalf("hit %d carried delivery ID %q, want the stable %q", i+1, id, deliveryID)
		}
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("DeliveryWorker.Run: %v", err)
	}
}
