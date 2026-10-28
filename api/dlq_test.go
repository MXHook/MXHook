package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mxhook/mxhook/api"
	"github.com/mxhook/mxhook/webhook"
)

func TestDLQHandler_ListsEntries(t *testing.T) {
	dlq := webhook.NewInMemoryDLQ()
	if err := dlq.Add(context.Background(), webhook.DLQEntry{
		MessageID:  "msg-1",
		WebhookURL: "https://example.com/hook",
		FailedAt:   time.Now(),
		Reason:     "500",
		Payload:    []byte(`{"id":"msg-1"}`),
	}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	srv := newTestServer(api.WithDLQ(dlq))
	req := authed(httptest.NewRequest(http.MethodGet, "/dlq", nil))
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", w.Code)
	}
	var entries []map[string]any
	if err := json.NewDecoder(w.Body).Decode(&entries); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
}
