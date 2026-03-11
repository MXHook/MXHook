package postgres_test

import (
	"testing"

	"github.com/mxhook/mxhook/queue"
	pgqueue "github.com/mxhook/mxhook/queue/postgres"
)

func TestAdapter_ImplementsEnqueuer(t *testing.T) {
	var _ queue.Enqueuer = (*pgqueue.Adapter)(nil)
}

func TestAdapter_ImplementsDequeuer(t *testing.T) {
	var _ queue.Dequeuer = (*pgqueue.Adapter)(nil)
}
