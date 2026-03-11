package webhook_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mxhook/mxhook/internal/types"
	"github.com/mxhook/mxhook/webhook"
)

func TestDelivery_SignsAndPosts(t *testing.T) {
	var received []byte
	var gotSig string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received, _ = io.ReadAll(r.Body)
		gotSig = r.Header.Get("X-MXHook-Signature")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	d := webhook.NewDeliverer(webhook.Config{
		MaxAttempts: 1,
		TimeoutSecs: 5,
	})

	event := &types.EmailEvent{
		ID:      "msg-1",
		Subject: "Test",
	}
	err := d.Deliver(context.Background(), webhook.Endpoint{
		URL:    srv.URL,
		Secret: "mysecret",
	}, event)
	if err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	if len(received) == 0 {
		t.Fatal("no payload received")
	}
	if gotSig == "" {
		t.Fatal("expected X-MXHook-Signature header")
	}
	if !webhook.VerifySignature(received, "mysecret", gotSig) {
		t.Error("signature verification failed")
	}
}

func TestDelivery_RetriesOnFailure(t *testing.T) {
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 2 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	d := webhook.NewDeliverer(webhook.Config{
		MaxAttempts: 3,
		TimeoutSecs: 5,
	})
	err := d.Deliver(context.Background(), webhook.Endpoint{URL: srv.URL, Secret: "s"}, &types.EmailEvent{ID: "x"})
	if err != nil {
		t.Fatalf("expected success after retry, got: %v", err)
	}
	if attempts != 2 {
		t.Errorf("expected 2 attempts, got %d", attempts)
	}
}
