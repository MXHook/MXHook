package webhook_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mxhook/mxhook/types"
	"github.com/mxhook/mxhook/webhook"
)

func TestDelivery_SignsAndPosts(t *testing.T) {
	var received []byte
	var gotSig, gotTS string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received, _ = io.ReadAll(r.Body)
		gotSig = r.Header.Get("X-MXHook-Signature")
		gotTS = r.Header.Get("X-MXHook-Timestamp")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	d := webhook.NewDeliverer(webhook.Config{
		MaxAttempts: 1, TimeoutSecs: 5,
		AllowHTTP: true, AllowPrivateNetwork: true,
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
	if gotTS == "" {
		t.Fatal("expected X-MXHook-Timestamp header")
	}
	if !webhook.VerifyTimestampedSignature(received, "mysecret", gotSig, gotTS, 5*time.Minute) {
		t.Error("timestamped signature verification failed")
	}
	if webhook.VerifySignature(received, "mysecret", gotSig) {
		t.Error("delivery still signed with the replayable legacy payload-only scheme")
	}
}

func TestDelivery_RetriesOnFailure(t *testing.T) {
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		if attempts < 2 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	d := webhook.NewDeliverer(webhook.Config{
		MaxAttempts: 3, TimeoutSecs: 5,
		AllowHTTP: true, AllowPrivateNetwork: true,
	})
	err := d.Deliver(context.Background(), webhook.Endpoint{URL: srv.URL, Secret: "s"}, &types.EmailEvent{ID: "x"})
	if err != nil {
		t.Fatalf("expected success after retry, got: %v", err)
	}
	if attempts != 2 {
		t.Errorf("expected 2 attempts, got %d", attempts)
	}
}
