package queue_test

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/mxhook/mxhook/queue"
)

func TestEnqueuerInterface(_ *testing.T) {
	// Compile-time check: Enqueuer interface is satisfied
	var _ queue.Enqueuer = (*queue.NoopEnqueuer)(nil)
}

func TestNoopEnqueue(t *testing.T) {
	q := &queue.NoopEnqueuer{}
	err := q.Enqueue(context.Background(), testMessage("test-1", []byte("raw email body")))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMessageCodecRoundTrip(t *testing.T) {
	want := queue.NewMessage(
		"message-1", "smtp", "", "sender@example.com",
		[]string{"one@example.com", "two@example.com"},
		time.Date(2026, time.August, 28, 12, 0, 0, 0, time.UTC),
		[]byte("Subject: test\r\n\r\nbody"),
	)
	encoded, err := queue.EncodeMessage(want)
	if err != nil {
		t.Fatalf("EncodeMessage: %v", err)
	}
	got, err := queue.DecodeMessage(encoded)
	if err != nil {
		t.Fatalf("DecodeMessage: %v", err)
	}
	if got.SchemaVersion != queue.MessageSchemaV1 || got.ID != want.ID || got.TraceID != want.TraceID ||
		got.Source != want.Source || got.EnvelopeFrom != want.EnvelopeFrom ||
		!got.ReceivedAt.Equal(want.ReceivedAt) || !bytes.Equal(got.Body, want.Body) {
		t.Fatalf("got = %#v, want %#v", got, want)
	}
	if len(got.EnvelopeTo) != 2 || got.EnvelopeTo[1] != "two@example.com" {
		t.Fatalf("recipients = %#v", got.EnvelopeTo)
	}
}

func TestMessageValidationRejectsIncompleteEnvelope(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*queue.Message)
		message string
	}{
		{name: "schema", mutate: func(m *queue.Message) { m.SchemaVersion = "" }, message: "schema"},
		{name: "id", mutate: func(m *queue.Message) { m.ID = "" }, message: "ID"},
		{name: "trace", mutate: func(m *queue.Message) { m.TraceID = "" }, message: "trace"},
		{name: "received", mutate: func(m *queue.Message) { m.ReceivedAt = time.Time{} }, message: "received"},
		{name: "source", mutate: func(m *queue.Message) { m.Source = "" }, message: "source"},
		{name: "recipients", mutate: func(m *queue.Message) { m.EnvelopeTo = nil }, message: "recipient"},
		{name: "body", mutate: func(m *queue.Message) { m.Body = nil }, message: "raw"},
		{name: "size", mutate: func(m *queue.Message) { m.RawSize++ }, message: "size"},
		{name: "digest", mutate: func(m *queue.Message) { m.RawSHA256 = "bad" }, message: "digest"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			message := testMessage("validation", []byte("body"))
			test.mutate(&message)
			if _, err := queue.EncodeMessage(message); err == nil || !strings.Contains(err.Error(), test.message) {
				t.Fatalf("EncodeMessage error = %v, want %s validation error", err, test.message)
			}
		})
	}
}

func TestNewReceiptHandleIsUniqueAndOpaque(t *testing.T) {
	seen := make(map[string]bool, 128)
	for range 128 {
		receipt, err := queue.NewReceiptHandle()
		if err != nil {
			t.Fatalf("NewReceiptHandle: %v", err)
		}
		if len(receipt) != 32 {
			t.Fatalf("receipt %q length = %d, want 32 hex characters", receipt, len(receipt))
		}
		if _, err := hex.DecodeString(receipt); err != nil {
			t.Fatalf("receipt %q is not hex: %v", receipt, err)
		}
		if seen[receipt] {
			t.Fatalf("duplicate receipt %q", receipt)
		}
		seen[receipt] = true
	}
}

func TestMatchesEncoded(t *testing.T) {
	msg := testMessage("canonical", []byte("body"))
	encoded, err := queue.EncodeMessage(msg)
	if err != nil {
		t.Fatalf("EncodeMessage: %v", err)
	}

	var indented bytes.Buffer
	if err := json.Indent(&indented, encoded, "", "    "); err != nil {
		t.Fatalf("Indent: %v", err)
	}
	if !queue.MatchesEncoded(indented.Bytes(), msg) {
		t.Error("reserialized payload does not match identical message")
	}

	differentBody := testMessage("canonical", []byte("other body"))
	if queue.MatchesEncoded(encoded, differentBody) {
		t.Error("stored payload matches message with different body")
	}
	differentRecipient := queue.NewMessage("canonical", "test", "", "sender@example.com",
		[]string{"other@example.com"}, time.Unix(1, 0), []byte("body"))
	if queue.MatchesEncoded(encoded, differentRecipient) {
		t.Error("stored payload matches message with different recipients")
	}
	if queue.MatchesEncoded([]byte("not json"), msg) {
		t.Error("undecodable stored payload matches")
	}
}

func testMessage(id string, body []byte) queue.Message {
	return queue.NewMessage(id, "test", "", "sender@example.com", []string{"inbox@example.com"}, time.Unix(1, 0), body)
}
