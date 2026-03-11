package webhook_test

import (
	"context"
	"testing"
	"time"

	"github.com/mxhook/mxhook/webhook"
)

func TestDLQ_AddAndList(t *testing.T) {
	dlq := webhook.NewInMemoryDLQ()

	entry := webhook.DLQEntry{
		MessageID:  "msg-1",
		WebhookURL: "https://example.com/hook",
		FailedAt:   time.Now(),
		Reason:     "server returned 500",
		Payload:    []byte(`{"id":"msg-1"}`),
	}

	if err := dlq.Add(context.Background(), entry); err != nil {
		t.Fatalf("Add: %v", err)
	}

	entries, err := dlq.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].MessageID != "msg-1" {
		t.Errorf("wrong MessageID: %q", entries[0].MessageID)
	}
}

func TestDLQ_Remove(t *testing.T) {
	dlq := webhook.NewInMemoryDLQ()
	entry := webhook.DLQEntry{MessageID: "msg-2", Payload: []byte("x")}
	dlq.Add(context.Background(), entry)

	if err := dlq.Remove(context.Background(), "msg-2"); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	entries, _ := dlq.List(context.Background())
	if len(entries) != 0 {
		t.Errorf("expected empty DLQ, got %d", len(entries))
	}
}
