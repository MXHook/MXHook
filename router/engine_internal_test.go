package router

import (
	"context"
	"testing"
	"time"
)

// TestEngine_Close_WithTicker tests that Close properly stops a ticker and
// closes the stop channel when both are non-nil (i.e., the Postgres-backed
// engine path). We construct the Engine directly since NewEngine requires a
// real DB.
func TestEngine_Close_WithTicker(t *testing.T) {
	e := &Engine{
		ticker: time.NewTicker(time.Hour),
		stop:   make(chan struct{}),
	}
	// Start a fake hotReload goroutine so the stop channel is consumed.
	done := make(chan struct{})
	go func() {
		defer close(done)
		e.hotReload(context.Background())
	}()

	e.Close()

	select {
	case <-done:
		// goroutine exited cleanly via stop channel
	case <-time.After(2 * time.Second):
		t.Fatal("hotReload goroutine did not exit after Close")
	}
}

// TestEngine_hotReload_ContextCancelled tests that hotReload exits when the
// context is cancelled.
func TestEngine_hotReload_ContextCancelled(t *testing.T) {
	e := &Engine{
		ticker: time.NewTicker(time.Hour),
		stop:   make(chan struct{}),
	}
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		defer close(done)
		e.hotReload(ctx)
	}()

	cancel()

	select {
	case <-done:
		// goroutine exited cleanly via ctx.Done()
	case <-time.After(2 * time.Second):
		t.Fatal("hotReload goroutine did not exit after context cancellation")
	}
}
