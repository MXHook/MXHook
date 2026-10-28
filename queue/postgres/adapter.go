package postgres

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mxhook/mxhook/queue"
)

//go:embed schema.sql
var schemaSQL string

const (
	defaultLeaseDuration = 30 * time.Second
	defaultPollInterval  = 500 * time.Millisecond
)

type Config struct {
	DSN           string
	LeaseDuration time.Duration
	PollInterval  time.Duration
	// RunMigrations applies the idempotent queue schema DDL at startup. When
	// false the schema must already exist; the constructor verifies it and
	// fails fast otherwise, so steady-state roles can run with a reduced
	// data-access identity.
	RunMigrations bool
}

// Adapter implements queue.Enqueuer and queue.Dequeuer using Postgres.
type Adapter struct {
	pool          *pgxpool.Pool
	leaseDuration time.Duration
	pollInterval  time.Duration
}

// New returns a new Adapter connected to the given Postgres DSN.
func New(ctx context.Context, dsn string) (*Adapter, error) {
	return NewWithConfig(ctx, Config{DSN: dsn, RunMigrations: true})
}

func NewWithConfig(ctx context.Context, cfg Config) (*Adapter, error) {
	if cfg.DSN == "" {
		return nil, errors.New("postgres queue: DSN is required")
	}
	if cfg.LeaseDuration == 0 {
		cfg.LeaseDuration = defaultLeaseDuration
	}
	if cfg.LeaseDuration < 0 {
		return nil, errors.New("postgres queue: lease duration must be positive")
	}
	if cfg.PollInterval == 0 {
		cfg.PollInterval = defaultPollInterval
	}
	if cfg.PollInterval < 0 {
		return nil, errors.New("postgres queue: poll interval must be positive")
	}
	pool, err := pgxpool.New(ctx, cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("postgres queue: connect: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("postgres queue: ping: %w", err)
	}
	if cfg.RunMigrations {
		if _, err := pool.Exec(ctx, schemaSQL); err != nil {
			pool.Close()
			return nil, fmt.Errorf("postgres queue: initialize schema: %w", err)
		}
	} else if err := verifySchema(ctx, pool); err != nil {
		pool.Close()
		return nil, err
	}
	return &Adapter{pool: pool, leaseDuration: cfg.LeaseDuration, pollInterval: cfg.PollInterval}, nil
}

func verifySchema(ctx context.Context, pool *pgxpool.Pool) error {
	var exists bool
	if err := pool.QueryRow(ctx, `SELECT to_regclass('mxhook_queue') IS NOT NULL`).Scan(&exists); err != nil {
		return fmt.Errorf("postgres queue: verify schema: %w", err)
	}
	if !exists {
		return errors.New("postgres queue: schema missing; run once with migrations enabled")
	}
	return nil
}

// Close shuts down the connection pool.
func (a *Adapter) Close() {
	a.pool.Close()
}

// Enqueue inserts a message into the queue table.
func (a *Adapter) Enqueue(ctx context.Context, msg queue.Message) error {
	payload, err := queue.EncodeMessage(msg)
	if err != nil {
		return fmt.Errorf("postgres queue: validate message: %w", err)
	}
	result, err := a.pool.Exec(ctx,
		`INSERT INTO mxhook_queue (id, payload) VALUES ($1, $2::jsonb)
		 ON CONFLICT (id) DO NOTHING`,
		msg.ID, payload,
	)
	if err != nil {
		return fmt.Errorf("postgres queue: enqueue: %w", err)
	}
	if result.RowsAffected() == 1 {
		return nil
	}

	var existingPayload []byte
	if err := a.pool.QueryRow(ctx,
		`SELECT payload FROM mxhook_queue WHERE id = $1`, msg.ID,
	).Scan(&existingPayload); err != nil {
		return fmt.Errorf("postgres queue: inspect duplicate: %w", err)
	}
	if queue.MatchesEncoded(existingPayload, msg) {
		return nil
	}
	return fmt.Errorf("%w: %s", queue.ErrIDCollision, msg.ID)
}

func (a *Adapter) Claim(ctx context.Context) (queue.Delivery, error) {
	for {
		delivery, err := a.claimOneLeased(ctx)
		if err != nil {
			return queue.Delivery{}, err
		}
		if delivery != nil {
			return *delivery, nil
		}
		timer := time.NewTimer(a.pollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return queue.Delivery{}, ctx.Err()
		case <-timer.C:
		}
	}
}

func (a *Adapter) claimOneLeased(ctx context.Context) (*queue.Delivery, error) {
	for {
		receipt, err := queue.NewReceiptHandle()
		if err != nil {
			return nil, fmt.Errorf("postgres queue: create lease receipt: %w", err)
		}
		var id string
		var delivery queue.Delivery
		var payload []byte
		// The lease deadline handed to callers comes from the Go clock,
		// captured before the claim runs. The database still enforces expiry
		// with its own NOW(), so under clock skew the returned deadline is
		// conservative and renewal scheduled from it (time.Until) never runs
		// later than the database-side expiry.
		claimedAt := time.Now()
		err = a.pool.QueryRow(ctx,
			`WITH candidate AS (
			   SELECT id FROM mxhook_queue
			   WHERE state = 'pending' OR
			         (state = 'processing' AND locked_until <= NOW())
			   ORDER BY created_at, id
			   LIMIT 1
			   FOR UPDATE SKIP LOCKED
			 )
			 UPDATE mxhook_queue AS q
			 SET state = 'processing', attempts = q.attempts + 1,
			     lease_token = $1,
			     locked_until = NOW() + make_interval(secs => $2),
			     updated_at = NOW()
			 FROM candidate
			 WHERE q.id = candidate.id
			 RETURNING q.id, q.payload, q.attempts`,
			receipt, a.leaseDuration.Seconds(),
		).Scan(
			&id,
			&payload,
			&delivery.Attempt,
		)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		if err != nil {
			return nil, fmt.Errorf("postgres queue: claim lease: %w", err)
		}
		delivery.LockedUntil = claimedAt.Add(a.leaseDuration)
		delivery.Message, err = queue.DecodeMessage(payload)
		if err != nil {
			// An undecodable payload can never be processed; erroring here
			// would crash the worker on every lease expiry. The claim above
			// already holds the lease, so nack it into quarantine and move on.
			if nackErr := a.Nack(ctx, id, receipt, err.Error()); nackErr != nil {
				return nil, fmt.Errorf("postgres queue: quarantine undecodable message %s: %w", id, nackErr)
			}
			continue
		}
		delivery.Receipt = receipt
		return &delivery, nil
	}
}

// Ack deletes the queue row outright: acked payloads are duplicated by the
// raw store and event tables, so retaining them only grows the database.
// Nacked rows are kept for the control API. A repeated acknowledgement of an
// already-deleted row remains a safe no-op.
func (a *Adapter) Ack(ctx context.Context, id, receipt string) error {
	result, err := a.pool.Exec(ctx,
		`DELETE FROM mxhook_queue
		 WHERE id = $1 AND lease_token = $2 AND state = 'processing' AND locked_until > NOW()`,
		id, receipt,
	)
	if err != nil {
		return fmt.Errorf("postgres queue: ack lease: %w", err)
	}
	if result.RowsAffected() == 1 {
		return nil
	}
	var exists bool
	if err := a.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM mxhook_queue WHERE id = $1)`, id,
	).Scan(&exists); err != nil {
		return fmt.Errorf("postgres queue: inspect ack: %w", err)
	}
	if !exists {
		return nil
	}
	return fmt.Errorf("%w: %s", queue.ErrLeaseLost, id)
}

func (a *Adapter) Nack(ctx context.Context, id, receipt, reason string) error {
	result, err := a.pool.Exec(ctx,
		`UPDATE mxhook_queue
		 SET state = 'nacked', failure_reason = $3,
		     locked_until = NULL, updated_at = NOW()
		 WHERE id = $1 AND lease_token = $2 AND state = 'processing' AND locked_until > NOW()`,
		id, receipt, reason,
	)
	if err != nil {
		return fmt.Errorf("postgres queue: nack lease: %w", err)
	}
	if result.RowsAffected() == 1 {
		return nil
	}
	var state string
	err = a.pool.QueryRow(ctx,
		`SELECT state FROM mxhook_queue WHERE id = $1 AND lease_token = $2`, id, receipt,
	).Scan(&state)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("postgres queue: inspect nack: %w", err)
	}
	if err == nil && state == "nacked" {
		return nil
	}
	return fmt.Errorf("%w: %s", queue.ErrLeaseLost, id)
}

func (a *Adapter) Extend(ctx context.Context, id, receipt string, extension time.Duration) (time.Time, error) {
	if extension <= 0 {
		return time.Time{}, errors.New("postgres queue: lease extension must be positive")
	}
	// Same clock authority as Claim: the returned deadline comes from the Go
	// clock captured before the update, so renewal scheduling stays
	// conservative relative to the database-enforced expiry.
	extendedAt := time.Now()
	result, err := a.pool.Exec(ctx,
		`UPDATE mxhook_queue
		 SET locked_until = NOW() + make_interval(secs => $3), updated_at = NOW()
		 WHERE id = $1 AND lease_token = $2 AND state = 'processing' AND locked_until > NOW()`,
		id, receipt, extension.Seconds(),
	)
	if err != nil {
		return time.Time{}, fmt.Errorf("postgres queue: extend lease: %w", err)
	}
	if result.RowsAffected() != 1 {
		return time.Time{}, fmt.Errorf("%w: %s", queue.ErrLeaseLost, id)
	}
	return extendedAt.Add(extension), nil
}
