package parser_test

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/mxhook/mxhook/internal/types"
	"github.com/mxhook/mxhook/parser"
	"github.com/mxhook/mxhook/queue"
)

// workerSampleEML is defined here to avoid redeclaration with parser_test.go
const workerSampleEML = `From: sender@example.com
To: recipient@example.com
Subject: Test email
Message-ID: <test-123@example.com>
MIME-Version: 1.0
Content-Type: text/plain

Hello, world!
`

type captureStore struct {
	stored map[string]string
}

func (c *captureStore) Store(_ context.Context, id string, r io.Reader) (string, error) {
	b, _ := io.ReadAll(r)
	c.stored[id] = string(b)
	return "key:" + id, nil
}

func (c *captureStore) Get(_ context.Context, _ string) ([]byte, error) { return nil, nil }
func (c *captureStore) Delete(_ context.Context, _ string) error        { return nil }

type captureEvents struct {
	events []*types.EmailEvent
}

func (c *captureEvents) Save(_ context.Context, e *types.EmailEvent) error {
	c.events = append(c.events, e)
	return nil
}

func TestWorker_ProcessesMessage(t *testing.T) {
	ch := make(chan queue.Message, 1)
	ch <- queue.Message{
		ID:   "msg-1",
		Body: []byte(workerSampleEML),
		Meta: map[string]string{
			"envelope_from": "sender@example.com",
			"envelope_to_0": "recipient@example.com",
		},
	}
	close(ch)

	store := &captureStore{stored: make(map[string]string)}
	events := &captureEvents{}
	p := parser.New()
	w := parser.NewWorker(p, store, events)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	w.Run(ctx, ch)

	if len(events.events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events.events))
	}
	if events.events[0].Subject != "Test email" {
		t.Errorf("unexpected subject: %q", events.events[0].Subject)
	}
}
