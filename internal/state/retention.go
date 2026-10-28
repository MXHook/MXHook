package state

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// RawDeleter removes one stored raw message file.
// *storage.FilesystemStorer satisfies it.
type RawDeleter interface {
	Delete(ctx context.Context, path string) error
}

// RetentionConfig bounds terminal state growth. Terminal rows older than TTL
// are deleted: succeeded deliveries (attempts cascade), events no delivery
// references any more (their raw files included), and ingress failures. Dead
// deliveries and the events they reference are always retained for the
// control API.
type RetentionConfig struct {
	// TTL is the minimum age before terminal rows are deleted. Required.
	TTL time.Duration
	// Interval is the sweep cadence. Defaults to one hour.
	Interval time.Duration
}

const (
	defaultRetentionInterval = time.Hour
	retentionBatchSize       = 500
)

// Janitor periodically deletes expired terminal state and orphaned raw files.
type Janitor struct {
	store    *Store
	raw      RawDeleter
	ttl      time.Duration
	interval time.Duration
}

// SweepStats counts what one sweep removed.
type SweepStats struct {
	Deliveries      int64
	Events          int64
	IngressFailures int64
	RawFiles        int64
}

func NewJanitor(store *Store, raw RawDeleter, cfg RetentionConfig) (*Janitor, error) {
	if store == nil {
		return nil, errors.New("state: retention store is required")
	}
	if cfg.TTL <= 0 {
		return nil, errors.New("state: retention TTL must be positive")
	}
	if cfg.Interval == 0 {
		cfg.Interval = defaultRetentionInterval
	}
	if cfg.Interval < 0 {
		return nil, errors.New("state: retention interval must be positive")
	}
	return &Janitor{store: store, raw: raw, ttl: cfg.TTL, interval: cfg.Interval}, nil
}

// Run sweeps on the configured interval until the context ends. Sweep errors
// are logged and retried on the next tick; they never stop the janitor.
func (j *Janitor) Run(ctx context.Context) {
	ticker := time.NewTicker(j.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			stats, err := j.SweepOnce(ctx)
			if err != nil {
				if ctx.Err() == nil {
					slog.Error("state: retention sweep", "err", err)
				}
				continue
			}
			if stats != (SweepStats{}) {
				slog.Info("state: retention sweep",
					"deliveries", stats.Deliveries, "events", stats.Events,
					"ingress_failures", stats.IngressFailures, "raw_files", stats.RawFiles)
			}
		}
	}
}

// SweepOnce deletes expired terminal rows and their raw files once.
func (j *Janitor) SweepOnce(ctx context.Context) (SweepStats, error) {
	var stats SweepStats
	cutoff := formatTime(j.store.now().UTC().Add(-j.ttl))

	result, err := j.store.db.ExecContext(ctx, j.store.bind(
		`DELETE FROM mxhook_deliveries WHERE state = 'succeeded' AND updated_at < ?`), cutoff)
	if err != nil {
		return stats, fmt.Errorf("state: sweep succeeded deliveries: %w", err)
	}
	if stats.Deliveries, err = result.RowsAffected(); err != nil {
		return stats, fmt.Errorf("state: sweep succeeded deliveries result: %w", err)
	}

	for {
		deleted, rawPaths, err := j.sweepEventBatch(ctx, cutoff)
		if err != nil {
			return stats, err
		}
		stats.Events += deleted
		stats.RawFiles += j.deleteRawFiles(ctx, rawPaths)
		if deleted < retentionBatchSize {
			break
		}
	}

	result, err = j.store.db.ExecContext(ctx, j.store.bind(
		`DELETE FROM mxhook_ingress_failures WHERE failed_at < ?`), cutoff)
	if err != nil {
		return stats, fmt.Errorf("state: sweep ingress failures: %w", err)
	}
	if stats.IngressFailures, err = result.RowsAffected(); err != nil {
		return stats, fmt.Errorf("state: sweep ingress failures result: %w", err)
	}
	return stats, nil
}

// sweepEventBatch deletes up to retentionBatchSize expired events that no
// delivery references any more, and returns their raw paths. Events joined to
// any remaining delivery (dead ones included) are kept: dead-letter replay
// still needs the payload.
func (j *Janitor) sweepEventBatch(ctx context.Context, cutoff string) (int64, []string, error) {
	rows, err := j.store.db.QueryContext(ctx, j.store.bind(
		`SELECT id, raw_path FROM mxhook_events e
		 WHERE received_at < ? AND NOT EXISTS (
		       SELECT 1 FROM mxhook_deliveries d WHERE d.message_id = e.id)
		 LIMIT ?`), cutoff, retentionBatchSize)
	if err != nil {
		return 0, nil, fmt.Errorf("state: select expired events: %w", err)
	}
	defer func() { _ = rows.Close() }() // iteration errors surface via rows.Err()
	var ids []any
	var rawPaths []string
	for rows.Next() {
		var id, rawPath string
		if err := rows.Scan(&id, &rawPath); err != nil {
			return 0, nil, fmt.Errorf("state: scan expired event: %w", err)
		}
		ids = append(ids, id)
		if rawPath != "" {
			rawPaths = append(rawPaths, rawPath)
		}
	}
	if err := rows.Err(); err != nil {
		return 0, nil, fmt.Errorf("state: select expired events: %w", err)
	}
	if len(ids) == 0 {
		return 0, nil, nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?, ", len(ids)), ", ")
	result, err := j.store.db.ExecContext(ctx, j.store.bind(
		`DELETE FROM mxhook_events WHERE id IN (`+placeholders+`)`), ids...)
	if err != nil {
		return 0, nil, fmt.Errorf("state: delete expired events: %w", err)
	}
	deleted, err := result.RowsAffected()
	if err != nil {
		return 0, nil, fmt.Errorf("state: delete expired events result: %w", err)
	}
	return deleted, rawPaths, nil
}

// deleteRawFiles removes raw files whose event rows are gone. Failures are
// logged and skipped: the rows are already deleted, and a stranded file is
// preferable to a failed sweep.
func (j *Janitor) deleteRawFiles(ctx context.Context, paths []string) int64 {
	if j.raw == nil {
		return 0
	}
	var deleted int64
	for _, path := range paths {
		if err := j.raw.Delete(ctx, path); err != nil {
			if ctx.Err() == nil {
				slog.Warn("state: retention raw file", "path", path, "err", err)
			}
			continue
		}
		deleted++
	}
	return deleted
}
