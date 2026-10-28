package webhook_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/mxhook/mxhook/types"
	"github.com/mxhook/mxhook/webhook"
)

func TestDelivererRejectsPrivateNetworkTargetByDefault(t *testing.T) {
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	deliverer := webhook.NewDeliverer(webhook.Config{MaxAttempts: 1, TimeoutSecs: 1})
	err := deliverer.Deliver(context.Background(), webhook.Endpoint{URL: server.URL, Secret: "secret"}, &types.EmailEvent{ID: "private"})
	if err == nil {
		t.Fatal("Deliver accepted a private-network target")
	}
	if hits.Load() != 0 {
		t.Fatalf("private target received %d requests", hits.Load())
	}
}

func TestDelivererRejectsEmptySigningSecret(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	deliverer := webhook.NewDeliverer(webhook.Config{MaxAttempts: 1, TimeoutSecs: 1})
	if err := deliverer.Deliver(context.Background(), webhook.Endpoint{URL: server.URL}, &types.EmailEvent{ID: "unsigned"}); err == nil {
		t.Fatal("Deliver accepted an empty signing secret")
	}
}
