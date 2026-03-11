package queue

import "context"

// Message is the unit of work passed through the queue.
type Message struct {
	ID   string
	Body []byte
	Meta map[string]string
}

// Enqueuer is the interface for adding messages to the queue.
type Enqueuer interface {
	Enqueue(ctx context.Context, msg Message) error
}

// Dequeuer is the interface for consuming messages from the queue.
type Dequeuer interface {
	Dequeue(ctx context.Context) (<-chan Message, error)
	Ack(ctx context.Context, id string) error
	Nack(ctx context.Context, id string, reason string) error
}

// NoopEnqueuer discards all messages. Useful in tests.
type NoopEnqueuer struct{}

func (n *NoopEnqueuer) Enqueue(_ context.Context, _ Message) error {
	return nil
}
