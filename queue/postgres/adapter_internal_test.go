package postgres

import (
	"context"
	"testing"
	"time"
)

func TestNewRejectsUnreachableDatabase(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	adapter, err := New(ctx, "postgres://mxhook:mxhook@127.0.0.1:1/mxhook?connect_timeout=1")
	if err == nil {
		adapter.Close()
		t.Fatal("New accepted an unreachable database")
	}
}
