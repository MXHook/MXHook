package webhook_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mxhook/mxhook/webhook"
)

func TestReplay_RedeliversFromDLQ(t *testing.T) {
	delivered := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		delivered = true
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	dlq := webhook.NewInMemoryDLQ()
	dlq.Add(context.Background(), webhook.DLQEntry{
		MessageID:  "msg-1",
		WebhookURL: srv.URL,
		FailedAt:   time.Now(),
		Payload:    []byte(`{"id":"msg-1"}`),
	})

	replayer := webhook.NewReplayer(dlq, webhook.NewDeliverer(webhook.Config{MaxAttempts: 1, TimeoutSecs: 5}))
	if err := replayer.Replay(context.Background(), "msg-1", ""); err != nil {
		t.Fatalf("Replay: %v", err)
	}
	if !delivered {
		t.Error("expected delivery after replay")
	}
}
