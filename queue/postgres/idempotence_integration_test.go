package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/mxhook/mxhook/queue"
)

func TestEnqueueIdempotenceIntegration(t *testing.T) {
	dsn := newIntegrationDSN(t)
	ctx := context.Background()
	adapter, err := New(ctx, dsn)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer adapter.Close()

	msg := integrationMessage("idempotence", []byte("one"))
	if err := adapter.Enqueue(ctx, msg); err != nil {
		t.Fatalf("first Enqueue: %v", err)
	}
	if err := adapter.Enqueue(ctx, msg); err != nil {
		t.Fatalf("duplicate Enqueue: %v", err)
	}
	different := integrationMessage(msg.ID, []byte("different"))
	if err := adapter.Enqueue(ctx, different); !errors.Is(err, queue.ErrIDCollision) {
		t.Fatalf("collision error = %v, want queue.ErrIDCollision", err)
	}
}

func TestEnqueueCollisionOnMetadataChangeIntegration(t *testing.T) {
	dsn := newIntegrationDSN(t)
	ctx := context.Background()
	adapter, err := New(ctx, dsn)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer adapter.Close()

	msg := integrationMessage("metadata", []byte("body"))
	if err := adapter.Enqueue(ctx, msg); err != nil {
		t.Fatalf("first Enqueue: %v", err)
	}
	changed := msg
	changed.EnvelopeTo = []string{"other@example.com"}
	if err := adapter.Enqueue(ctx, changed); !errors.Is(err, queue.ErrIDCollision) {
		t.Fatalf("collision error = %v, want queue.ErrIDCollision", err)
	}
}
