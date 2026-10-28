package webhook_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mxhook/mxhook/types"
	"github.com/mxhook/mxhook/webhook"
)

// ---------------------------------------------------------------------------
// NewDeliverer defaults
// ---------------------------------------------------------------------------

func TestNewDeliverer_Defaults(t *testing.T) {
	// Passing zero-value Config should apply defaults (MaxAttempts=3, TimeoutSecs=30).
	d := webhook.NewDeliverer(webhook.Config{})
	if d.Timeout() != 30*time.Second {
		t.Errorf("expected 30s timeout, got %v", d.Timeout())
	}
}

// ---------------------------------------------------------------------------
// Delivery retry exhaustion — all attempts fail
// ---------------------------------------------------------------------------

func TestDelivery_AllAttemptsFail(t *testing.T) {
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	d := webhook.NewDeliverer(webhook.Config{MaxAttempts: 3, TimeoutSecs: 5, AllowHTTP: true, AllowPrivateNetwork: true})
	err := d.Deliver(context.Background(), webhook.Endpoint{URL: srv.URL, Secret: "s"}, &types.EmailEvent{ID: "x"})
	if err == nil {
		t.Fatal("expected error after all attempts fail")
	}
	if attempts != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts)
	}
	if !strings.Contains(err.Error(), "all 3 attempts failed") {
		t.Errorf("unexpected error message: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Delivery context cancellation
// ---------------------------------------------------------------------------

func TestDelivery_ContextCancelled(t *testing.T) {
	// Server always fails so we enter the retry wait; cancel there.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())

	// Cancel after the first failed attempt so we hit the ctx.Done() branch.
	d := webhook.NewDeliverer(webhook.Config{MaxAttempts: 5, TimeoutSecs: 5, AllowHTTP: true, AllowPrivateNetwork: true})

	var wg sync.WaitGroup
	wg.Add(1)
	var deliverErr error
	go func() {
		defer wg.Done()
		deliverErr = d.Deliver(ctx, webhook.Endpoint{URL: srv.URL, Secret: "s"}, &types.EmailEvent{ID: "cancel-test"})
	}()

	// Give the first attempt time to fail and enter the backoff wait, then cancel.
	time.Sleep(50 * time.Millisecond)
	cancel()
	wg.Wait()

	if deliverErr == nil {
		t.Fatal("expected context cancellation error")
	}
	if deliverErr != context.Canceled {
		t.Errorf("expected context.Canceled, got: %v", deliverErr)
	}
}

// ---------------------------------------------------------------------------
// Delivery with large response body (> 4096 bytes)
// ---------------------------------------------------------------------------

func TestDelivery_LargeResponseBody(t *testing.T) {
	body := bytes.Repeat([]byte("x"), 8192)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	d := webhook.NewDeliverer(webhook.Config{MaxAttempts: 1, TimeoutSecs: 5, AllowHTTP: true, AllowPrivateNetwork: true})
	err := d.Deliver(context.Background(), webhook.Endpoint{URL: srv.URL, Secret: "s"}, &types.EmailEvent{ID: "big"})
	if err != nil {
		t.Fatalf("expected success with large response body, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Delivery with permanent non-2xx that must not retry (e.g. 400 Bad Request)
// ---------------------------------------------------------------------------

func TestDelivery_Permanent4xxIsNotRetried(t *testing.T) {
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		w.WriteHeader(http.StatusBadRequest) // 400
	}))
	defer srv.Close()

	d := webhook.NewDeliverer(webhook.Config{MaxAttempts: 2, TimeoutSecs: 5, AllowHTTP: true, AllowPrivateNetwork: true})
	err := d.Deliver(context.Background(), webhook.Endpoint{URL: srv.URL, Secret: "s"}, &types.EmailEvent{ID: "bad"})
	if err == nil {
		t.Fatal("expected error for non-2xx")
	}
	if attempts != 1 {
		t.Errorf("expected 1 attempt for permanent 4xx, got %d", attempts)
	}
}

// ---------------------------------------------------------------------------
// Signer with empty secret
// ---------------------------------------------------------------------------

func TestSign_EmptySecret(t *testing.T) {
	payload := []byte(`{"id":"msg-1"}`)
	sig := webhook.Sign(payload, "")
	if sig == "" {
		t.Fatal("expected non-empty signature even with empty secret")
	}
	if !strings.HasPrefix(sig, "sha256=") {
		t.Errorf("unexpected signature format: %q", sig)
	}
	// HMAC with empty key should still be consistent.
	if sig2 := webhook.Sign(payload, ""); sig != sig2 {
		t.Error("empty-secret signatures are not consistent")
	}
	// Must NOT verify against a different secret.
	if webhook.VerifySignature(payload, "notempty", sig) {
		t.Error("empty-secret signature should not verify with non-empty secret")
	}
}

// ---------------------------------------------------------------------------
// DLQ: Get on missing entry returns error
// ---------------------------------------------------------------------------

func TestDLQ_GetMissing(t *testing.T) {
	dlq := webhook.NewInMemoryDLQ()
	_, err := dlq.Get(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for missing DLQ entry")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("unexpected error message: %v", err)
	}
}

// ---------------------------------------------------------------------------
// DLQ: concurrent writes don't race
// ---------------------------------------------------------------------------

func TestDLQ_ConcurrentWrites(t *testing.T) {
	dlq := webhook.NewInMemoryDLQ()
	var wg sync.WaitGroup
	n := 100
	wg.Add(n)
	for i := 0; i < n; i++ {
		i := i
		go func() {
			defer wg.Done()
			entry := webhook.DLQEntry{
				MessageID: strings.Repeat("x", i%10) + string(rune('A'+i%26)),
				Payload:   []byte("payload"),
			}
			_ = dlq.Add(context.Background(), entry)
		}()
	}
	wg.Wait()
	entries, err := dlq.List(context.Background())
	if err != nil {
		t.Fatalf("List after concurrent writes: %v", err)
	}
	if len(entries) == 0 {
		t.Error("expected entries after concurrent writes")
	}
}

// ---------------------------------------------------------------------------
// post: bad URL returns error (covers request creation error in post())
// ---------------------------------------------------------------------------

func TestDelivery_BadURL(t *testing.T) {
	d := webhook.NewDeliverer(webhook.Config{MaxAttempts: 1, TimeoutSecs: 5})
	err := d.Deliver(context.Background(), webhook.Endpoint{URL: "://bad-url", Secret: "s"}, &types.EmailEvent{ID: "x"})
	if err == nil {
		t.Fatal("expected error for malformed URL")
	}
}
