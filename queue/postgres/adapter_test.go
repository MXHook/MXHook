package postgres_test

import (
	"testing"

	"github.com/mxhook/mxhook/queue"
	pgqueue "github.com/mxhook/mxhook/queue/postgres"
)

func TestAdapter_ImplementsEnqueuer(_ *testing.T) {
	var _ queue.Enqueuer = (*pgqueue.Adapter)(nil)
}

func TestAdapter_ImplementsDequeuer(_ *testing.T) {
	var _ queue.Dequeuer = (*pgqueue.Adapter)(nil)
}
