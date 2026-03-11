package lifecycle_test

import (
	"context"
	"testing"
	"time"

	"github.com/mxhook/mxhook/internal/lifecycle"
)

func TestLogger_RecordsTransitions(t *testing.T) {
	var captured []lifecycle.Event
	store := &fakeStore{fn: func(e lifecycle.Event) { captured = append(captured, e) }}
	logger := lifecycle.NewLogger(store)

	logger.Record(context.Background(), lifecycle.Event{
		MessageID: "msg-1",
		State:     lifecycle.StateQueued,
		At:        time.Now(),
	})

	if len(captured) != 1 {
		t.Fatalf("expected 1 event, got %d", len(captured))
	}
	if captured[0].State != lifecycle.StateQueued {
		t.Errorf("wrong state: %v", captured[0].State)
	}
}

func TestLogger_SetsTimestampIfZero(t *testing.T) {
	var captured []lifecycle.Event
	store := &fakeStore{fn: func(e lifecycle.Event) { captured = append(captured, e) }}
	logger := lifecycle.NewLogger(store)

	logger.Record(context.Background(), lifecycle.Event{
		MessageID: "msg-2",
		State:     lifecycle.StateParsed,
		// At is zero
	})

	if captured[0].At.IsZero() {
		t.Error("expected At to be set automatically")
	}
}

type fakeStore struct{ fn func(lifecycle.Event) }

func (f *fakeStore) Save(_ context.Context, e lifecycle.Event) error {
	f.fn(e)
	return nil
}
