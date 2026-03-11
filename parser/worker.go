package parser

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"

	"github.com/mxhook/mxhook/internal/types"
	"github.com/mxhook/mxhook/queue"
)

// RawStorer stores raw email bytes.
type RawStorer interface {
	Store(ctx context.Context, id string, r io.Reader) (string, error)
	Get(ctx context.Context, path string) ([]byte, error)
	Delete(ctx context.Context, path string) error
}

// EventSaver persists a parsed EmailEvent.
type EventSaver interface {
	Save(ctx context.Context, event *types.EmailEvent) error
}

// Worker consumes raw messages from the queue and parses them.
type Worker struct {
	parser *Parser
	store  RawStorer
	saver  EventSaver
}

// NewWorker creates a Worker.
func NewWorker(p *Parser, store RawStorer, saver EventSaver) *Worker {
	return &Worker{parser: p, store: store, saver: saver}
}

// Run processes messages from ch until ctx is done or ch is closed.
func (w *Worker) Run(ctx context.Context, ch <-chan queue.Message) {
	for {
		select {
		case msg, ok := <-ch:
			if !ok {
				return
			}
			if err := w.process(ctx, msg); err != nil {
				slog.Error("worker: process failed", "id", msg.ID, "err", err)
			}
		case <-ctx.Done():
			return
		}
	}
}

func (w *Worker) process(ctx context.Context, msg queue.Message) error {
	from := msg.Meta["envelope_from"]
	var to []string
	for i := 0; ; i++ {
		key := fmt.Sprintf("envelope_to_%d", i)
		v, ok := msg.Meta[key]
		if !ok {
			break
		}
		to = append(to, v)
	}

	rawKey, err := w.store.Store(ctx, msg.ID, bytes.NewReader(msg.Body))
	if err != nil {
		return fmt.Errorf("worker: store raw: %w", err)
	}

	event, err := w.parser.Parse(msg.ID, from, to, bytes.NewReader(msg.Body))
	if err != nil {
		return fmt.Errorf("worker: parse: %w", err)
	}
	event.RawPath = rawKey

	return w.saver.Save(ctx, event)
}
