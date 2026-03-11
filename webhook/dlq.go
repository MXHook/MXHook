package webhook

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// DLQEntry holds a permanently failed delivery.
type DLQEntry struct {
	MessageID  string
	WebhookURL string
	FailedAt   time.Time
	Reason     string
	Payload    []byte
}

// DLQ is the interface for dead-letter queue operations.
type DLQ interface {
	Add(ctx context.Context, entry DLQEntry) error
	List(ctx context.Context) ([]DLQEntry, error)
	Get(ctx context.Context, messageID string) (DLQEntry, error)
	Remove(ctx context.Context, messageID string) error
}

// InMemoryDLQ is an in-memory DLQ implementation. Suitable for testing.
type InMemoryDLQ struct {
	mu      sync.RWMutex
	entries map[string]DLQEntry
}

// NewInMemoryDLQ creates an InMemoryDLQ.
func NewInMemoryDLQ() *InMemoryDLQ {
	return &InMemoryDLQ{entries: make(map[string]DLQEntry)}
}

func (d *InMemoryDLQ) Add(_ context.Context, entry DLQEntry) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.entries[entry.MessageID] = entry
	return nil
}

func (d *InMemoryDLQ) List(_ context.Context) ([]DLQEntry, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	out := make([]DLQEntry, 0, len(d.entries))
	for _, e := range d.entries {
		out = append(out, e)
	}
	return out, nil
}

func (d *InMemoryDLQ) Get(_ context.Context, messageID string) (DLQEntry, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	e, ok := d.entries[messageID]
	if !ok {
		return DLQEntry{}, fmt.Errorf("dlq: message %q not found", messageID)
	}
	return e, nil
}

func (d *InMemoryDLQ) Remove(_ context.Context, messageID string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.entries, messageID)
	return nil
}
