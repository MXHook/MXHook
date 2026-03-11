package lifecycle

import (
	"context"
	"log/slog"
	"time"
)

// State represents a message's lifecycle state.
type State string

const (
	StateAccepted  State = "accepted"
	StateQueued    State = "queued"
	StateParsed    State = "parsed"
	StateRouted    State = "routed"
	StateDelivered State = "delivered"
	StateFailed    State = "failed"
	StateRetried   State = "retried"
	StateDLQ       State = "dlq"
)

// Event is a single lifecycle transition.
type Event struct {
	MessageID string
	State     State
	At        time.Time
	Detail    string
}

// EventStore persists lifecycle events.
type EventStore interface {
	Save(ctx context.Context, e Event) error
}

// Logger records message lifecycle transitions.
type Logger struct {
	store EventStore
}

// NewLogger creates a Logger.
func NewLogger(store EventStore) *Logger {
	return &Logger{store: store}
}

// Record saves a lifecycle transition. Errors are logged but not propagated.
func (l *Logger) Record(ctx context.Context, e Event) {
	if e.At.IsZero() {
		e.At = time.Now().UTC()
	}
	if err := l.store.Save(ctx, e); err != nil {
		slog.Error("lifecycle: save failed", "message_id", e.MessageID, "state", e.State, "err", err)
	}
}
