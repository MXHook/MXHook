package queue_test

import (
	"context"
	"testing"

	"github.com/mxhook/mxhook/queue"
)

func TestEnqueuerInterface(t *testing.T) {
	// Compile-time check: Enqueuer interface is satisfied
	var _ queue.Enqueuer = (*queue.NoopEnqueuer)(nil)
}

func TestNoopEnqueue(t *testing.T) {
	q := &queue.NoopEnqueuer{}
	err := q.Enqueue(context.Background(), queue.Message{
		ID:   "test-1",
		Body: []byte("raw email body"),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
