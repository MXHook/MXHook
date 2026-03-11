package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mxhook/mxhook/queue"
)

// Adapter implements queue.Enqueuer and queue.Dequeuer using Postgres.
type Adapter struct {
	pool *pgxpool.Pool
}

// New returns a new Adapter connected to the given Postgres DSN.
func New(ctx context.Context, dsn string) (*Adapter, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("postgres queue: connect: %w", err)
	}
	return &Adapter{pool: pool}, nil
}

// Close shuts down the connection pool.
func (a *Adapter) Close() {
	a.pool.Close()
}

// Enqueue inserts a message into the queue table.
func (a *Adapter) Enqueue(ctx context.Context, msg queue.Message) error {
	meta, err := json.Marshal(msg.Meta)
	if err != nil {
		return fmt.Errorf("postgres queue: marshal meta: %w", err)
	}
	_, err = a.pool.Exec(ctx,
		`INSERT INTO mxhook_queue (id, body, meta) VALUES ($1, $2, $3)`,
		msg.ID, msg.Body, meta,
	)
	if err != nil {
		return fmt.Errorf("postgres queue: enqueue: %w", err)
	}
	return nil
}

// Dequeue returns a channel that yields pending messages one at a time.
// Messages are locked using SELECT FOR UPDATE SKIP LOCKED.
func (a *Adapter) Dequeue(ctx context.Context) (<-chan queue.Message, error) {
	ch := make(chan queue.Message)
	go func() {
		defer close(ch)
		for {
			msg, err := a.dequeueOne(ctx)
			if err != nil {
				return
			}
			if msg == nil {
				time.Sleep(500 * time.Millisecond)
				continue
			}
			select {
			case ch <- *msg:
			case <-ctx.Done():
				return
			}
		}
	}()
	return ch, nil
}

func (a *Adapter) dequeueOne(ctx context.Context) (*queue.Message, error) {
	tx, err := a.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("postgres queue: begin: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var (
		id   string
		body []byte
		meta []byte
	)
	err = tx.QueryRow(ctx,
		`SELECT id, body, meta FROM mxhook_queue
		 WHERE state = 'pending'
		 ORDER BY created_at
		 LIMIT 1
		 FOR UPDATE SKIP LOCKED`,
	).Scan(&id, &body, &meta)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("postgres queue: select: %w", err)
	}

	_, err = tx.Exec(ctx,
		`UPDATE mxhook_queue SET state = 'processing', attempts = attempts + 1, updated_at = NOW() WHERE id = $1`,
		id,
	)
	if err != nil {
		return nil, fmt.Errorf("postgres queue: update state: %w", err)
	}

	if err = tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("postgres queue: commit: %w", err)
	}

	var metaMap map[string]string
	if err = json.Unmarshal(meta, &metaMap); err != nil {
		metaMap = nil
	}

	return &queue.Message{ID: id, Body: body, Meta: metaMap}, nil
}

// Ack marks a message as successfully processed.
func (a *Adapter) Ack(ctx context.Context, id string) error {
	_, err := a.pool.Exec(ctx,
		`UPDATE mxhook_queue SET state = 'acked', updated_at = NOW() WHERE id = $1`,
		id,
	)
	if err != nil {
		return fmt.Errorf("postgres queue: ack: %w", err)
	}
	return nil
}

// Nack marks a message as failed with a reason.
func (a *Adapter) Nack(ctx context.Context, id string, reason string) error {
	meta, _ := json.Marshal(map[string]string{"nack_reason": reason})
	_, err := a.pool.Exec(ctx,
		`UPDATE mxhook_queue SET state = 'nacked', meta = meta || $2::jsonb, updated_at = NOW() WHERE id = $1`,
		id, meta,
	)
	if err != nil {
		return fmt.Errorf("postgres queue: nack: %w", err)
	}
	return nil
}
