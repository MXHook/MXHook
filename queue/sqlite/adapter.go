package sqlite

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/mxhook/mxhook/queue"
	// Registers the "sqlite" database/sql driver (pure Go).
	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schemaSQL string

var ErrIDCollision = queue.ErrIDCollision

const (
	defaultLeaseDuration = 30 * time.Second
	defaultPollInterval  = 500 * time.Millisecond
)

type Config struct {
	Path          string
	LeaseDuration time.Duration
	PollInterval  time.Duration
	// RunMigrations applies the idempotent queue schema DDL at startup. When
	// false the schema must already exist; the constructor verifies it and
	// fails fast otherwise, so steady-state roles can run with a reduced
	// data-access identity.
	RunMigrations bool
}

type Adapter struct {
	db            *sql.DB
	leaseDuration time.Duration
	pollInterval  time.Duration
	now           func() time.Time
}

func New(ctx context.Context, path string) (*Adapter, error) {
	return NewWithConfig(ctx, Config{Path: path, RunMigrations: true})
}

func NewWithConfig(ctx context.Context, cfg Config) (*Adapter, error) {
	if cfg.Path == "" {
		return nil, errors.New("sqlite queue: database path is required")
	}
	if cfg.LeaseDuration == 0 {
		cfg.LeaseDuration = defaultLeaseDuration
	}
	if cfg.LeaseDuration < 0 {
		return nil, errors.New("sqlite queue: lease duration must be positive")
	}
	if cfg.PollInterval == 0 {
		cfg.PollInterval = defaultPollInterval
	}
	if cfg.PollInterval < 0 {
		return nil, errors.New("sqlite queue: poll interval must be positive")
	}
	absPath, err := filepath.Abs(cfg.Path)
	if err != nil {
		return nil, fmt.Errorf("sqlite queue: resolve path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(absPath), 0o700); err != nil {
		return nil, fmt.Errorf("sqlite queue: create database directory: %w", err)
	}

	dsn := (&url.URL{Scheme: "file", Path: absPath}).String() +
		"?_pragma=busy_timeout(5000)" +
		"&_pragma=journal_mode(WAL)" +
		"&_pragma=foreign_keys(1)" +
		"&_txlock=immediate"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("sqlite queue: open: %w", err)
	}
	// SQLite permits one writer. Serializing the pool prevents competing Go
	// connections from turning normal concurrency into SQLITE_BUSY errors.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close() // best-effort cleanup on error path; original error returned
		return nil, fmt.Errorf("sqlite queue: ping: %w", err)
	}
	if cfg.RunMigrations {
		if _, err := db.ExecContext(ctx, schemaSQL); err != nil {
			_ = db.Close() // best-effort cleanup on error path; original error returned
			return nil, fmt.Errorf("sqlite queue: initialize schema: %w", err)
		}
	} else if err := verifySchema(ctx, db); err != nil {
		_ = db.Close() // best-effort cleanup on error path; original error returned
		return nil, err
	}
	if err := secureSQLiteFiles(absPath); err != nil {
		_ = db.Close() // best-effort cleanup on error path; original error returned
		return nil, err
	}
	return &Adapter{
		db:            db,
		leaseDuration: cfg.LeaseDuration,
		pollInterval:  cfg.PollInterval,
		now:           time.Now,
	}, nil
}

func verifySchema(ctx context.Context, db *sql.DB) error {
	var name string
	err := db.QueryRowContext(ctx,
		`SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'mxhook_queue'`,
	).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return errors.New("sqlite queue: schema missing; run once with migrations enabled")
	}
	if err != nil {
		return fmt.Errorf("sqlite queue: verify schema: %w", err)
	}
	return nil
}

func secureSQLiteFiles(path string) error {
	for _, candidate := range []string{path, path + "-wal", path + "-shm"} {
		if err := os.Chmod(candidate, 0o600); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("sqlite queue: secure %s: %w", filepath.Base(candidate), err)
		}
	}
	return nil
}

func (a *Adapter) Close() error {
	if err := a.db.Close(); err != nil {
		return fmt.Errorf("sqlite queue: close: %w", err)
	}
	return nil
}

// Depth reports the number of messages still awaiting or undergoing
// processing. The queue owns this count; readers must not query its tables
// directly.
func (a *Adapter) Depth(ctx context.Context) (int64, error) {
	var depth int64
	if err := a.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM mxhook_queue WHERE state IN ('pending', 'processing')`,
	).Scan(&depth); err != nil {
		return 0, fmt.Errorf("sqlite queue: depth: %w", err)
	}
	return depth, nil
}

// State reports the stored lifecycle state of a message, for assertions that
// must not race lease expiry.
func (a *Adapter) State(ctx context.Context, id string) (string, error) {
	var state string
	if err := a.db.QueryRowContext(ctx, `SELECT state FROM mxhook_queue WHERE id = ?`, id).Scan(&state); err != nil {
		return "", fmt.Errorf("sqlite queue: state: %w", err)
	}
	return state, nil
}

func (a *Adapter) Enqueue(ctx context.Context, msg queue.Message) error {
	payload, err := queue.EncodeMessage(msg)
	if err != nil {
		return fmt.Errorf("sqlite queue: validate message: %w", err)
	}
	result, err := a.db.ExecContext(ctx,
		`INSERT INTO mxhook_queue (id, payload)
		 VALUES (?, ?)
		 ON CONFLICT(id) DO NOTHING`,
		msg.ID, payload,
	)
	if err != nil {
		return fmt.Errorf("sqlite queue: enqueue: %w", err)
	}
	inserted, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("sqlite queue: enqueue result: %w", err)
	}
	if inserted == 1 {
		return nil
	}

	var existingPayload []byte
	if err := a.db.QueryRowContext(ctx,
		`SELECT payload FROM mxhook_queue WHERE id = ?`, msg.ID,
	).Scan(&existingPayload); err != nil {
		return fmt.Errorf("sqlite queue: inspect duplicate: %w", err)
	}
	if queue.MatchesEncoded(existingPayload, msg) {
		return nil
	}
	return fmt.Errorf("%w: %s", ErrIDCollision, msg.ID)
}

func (a *Adapter) Claim(ctx context.Context) (queue.Delivery, error) {
	for {
		delivery, err := a.claimOne(ctx)
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

func (a *Adapter) claimOne(ctx context.Context) (*queue.Delivery, error) {
	for {
		delivery, quarantined, err := a.claimCandidate(ctx)
		if err != nil {
			return nil, err
		}
		if !quarantined {
			return delivery, nil
		}
	}
}

func (a *Adapter) claimCandidate(ctx context.Context) (*queue.Delivery, bool, error) {
	now := a.now().UTC()
	lockedUntil := now.Add(a.leaseDuration)
	receipt, err := queue.NewReceiptHandle()
	if err != nil {
		return nil, false, fmt.Errorf("sqlite queue: create lease receipt: %w", err)
	}
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, false, fmt.Errorf("sqlite queue: begin lease claim: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // no-op after Commit; Commit error is the one reported

	var id string
	var delivery queue.Delivery
	var payload []byte
	err = tx.QueryRowContext(ctx,
		`SELECT id, payload, attempts
		 FROM mxhook_queue
		 WHERE state = 'pending'
		    OR (state = 'processing' AND julianday(locked_until) <= julianday(?))
		 ORDER BY created_at, id
		 LIMIT 1`, formatTime(now),
	).Scan(&id, &payload, &delivery.Attempt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("sqlite queue: select lease candidate: %w", err)
	}
	delivery.Message, err = queue.DecodeMessage(payload)
	if err != nil {
		// An undecodable payload can never be processed; erroring here would
		// keep reselecting the same row and wedge the queue.
		if err := a.quarantineCandidate(ctx, tx, id, err.Error(), now); err != nil {
			return nil, false, err
		}
		if err := tx.Commit(); err != nil {
			return nil, false, fmt.Errorf("sqlite queue: commit quarantine: %w", err)
		}
		return nil, true, nil
	}
	result, err := tx.ExecContext(ctx,
		`UPDATE mxhook_queue
		 SET state = 'processing', attempts = attempts + 1,
		     lease_token = ?, locked_until = ?, updated_at = ?
		 WHERE id = ? AND
		       (state = 'pending' OR
		        (state = 'processing' AND julianday(locked_until) <= julianday(?)))`,
		receipt, formatTime(lockedUntil), formatTime(now), id, formatTime(now),
	)
	if err != nil {
		return nil, false, fmt.Errorf("sqlite queue: claim lease: %w", err)
	}
	claimed, err := result.RowsAffected()
	if err != nil {
		return nil, false, fmt.Errorf("sqlite queue: claim lease result: %w", err)
	}
	if claimed != 1 {
		return nil, false, nil
	}
	delivery.Attempt++
	delivery.Receipt = receipt
	delivery.LockedUntil = lockedUntil
	if err := tx.Commit(); err != nil {
		return nil, false, fmt.Errorf("sqlite queue: commit lease claim: %w", err)
	}
	return &delivery, false, nil
}

func (a *Adapter) quarantineCandidate(ctx context.Context, tx *sql.Tx, id, reason string, now time.Time) error {
	result, err := tx.ExecContext(ctx,
		`UPDATE mxhook_queue
		 SET state = 'nacked', attempts = attempts + 1, failure_reason = ?,
		     locked_until = NULL, updated_at = ?
		 WHERE id = ? AND
		       (state = 'pending' OR
		        (state = 'processing' AND julianday(locked_until) <= julianday(?)))`,
		reason, formatTime(now), id, formatTime(now),
	)
	if err != nil {
		return fmt.Errorf("sqlite queue: quarantine undecodable message: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("sqlite queue: quarantine result: %w", err)
	}
	// A zero-row quarantine must not continue the claim loop: the same row
	// would be reselected forever.
	if rows != 1 {
		return fmt.Errorf("sqlite queue: quarantine undecodable message: %s not updated", id)
	}
	return nil
}

// Ack deletes the queue row outright: acked payloads are duplicated by the
// raw store and event tables, so retaining them only grows the database.
// Nacked rows are kept for the control API. A repeated acknowledgement of an
// already-deleted row remains a safe no-op.
func (a *Adapter) Ack(ctx context.Context, id, receipt string) error {
	now := a.now().UTC()
	result, err := a.db.ExecContext(ctx,
		`DELETE FROM mxhook_queue
		 WHERE id = ? AND lease_token = ? AND state = 'processing' AND
		       julianday(locked_until) > julianday(?)`,
		id, receipt, formatTime(now),
	)
	if err != nil {
		return fmt.Errorf("sqlite queue: ack lease: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("sqlite queue: lease result: %w", err)
	}
	if rows == 1 {
		return nil
	}
	var exists bool
	if err := a.db.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM mxhook_queue WHERE id = ?)`, id,
	).Scan(&exists); err != nil {
		return fmt.Errorf("sqlite queue: inspect ack: %w", err)
	}
	if !exists {
		return nil
	}
	return fmt.Errorf("%w: %s", queue.ErrLeaseLost, id)
}

func (a *Adapter) Nack(ctx context.Context, id, receipt, reason string) error {
	now := a.now().UTC()
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("sqlite queue: begin lease nack: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // no-op after Commit; Commit error is the one reported
	var state string
	var active bool
	if err := tx.QueryRowContext(ctx,
		`SELECT state,
		        state = 'processing' AND julianday(locked_until) > julianday(?)
		 FROM mxhook_queue WHERE id = ? AND lease_token = ?`,
		formatTime(now), id, receipt,
	).Scan(&state, &active); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: %s", queue.ErrLeaseLost, id)
		}
		return fmt.Errorf("sqlite queue: select lease nack: %w", err)
	}
	if state == "nacked" {
		return nil
	}
	if !active {
		return fmt.Errorf("%w: %s", queue.ErrLeaseLost, id)
	}
	result, err := tx.ExecContext(ctx,
		`UPDATE mxhook_queue
		 SET state = 'nacked', failure_reason = ?, locked_until = NULL, updated_at = ?
		 WHERE id = ? AND lease_token = ? AND state = 'processing' AND
		       julianday(locked_until) > julianday(?)`,
		reason, formatTime(now), id, receipt, formatTime(now),
	)
	if err != nil {
		return fmt.Errorf("sqlite queue: nack lease: %w", err)
	}
	if err := requireLeaseRow(result, id); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("sqlite queue: commit lease nack: %w", err)
	}
	return nil
}

func (a *Adapter) Extend(ctx context.Context, id, receipt string, extension time.Duration) (time.Time, error) {
	if extension <= 0 {
		return time.Time{}, errors.New("sqlite queue: lease extension must be positive")
	}
	now := a.now().UTC()
	lockedUntil := now.Add(extension)
	result, err := a.db.ExecContext(ctx,
		`UPDATE mxhook_queue SET locked_until = ?, updated_at = ?
		 WHERE id = ? AND lease_token = ? AND state = 'processing' AND
		       julianday(locked_until) > julianday(?)`,
		formatTime(lockedUntil), formatTime(now), id, receipt, formatTime(now),
	)
	if err != nil {
		return time.Time{}, fmt.Errorf("sqlite queue: extend lease: %w", err)
	}
	if err := requireLeaseRow(result, id); err != nil {
		return time.Time{}, err
	}
	return lockedUntil, nil
}

func requireLeaseRow(result sql.Result, id string) error {
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("sqlite queue: lease result: %w", err)
	}
	if rows != 1 {
		return fmt.Errorf("%w: %s", queue.ErrLeaseLost, id)
	}
	return nil
}

func formatTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}
