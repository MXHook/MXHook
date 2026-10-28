package state

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/mxhook/mxhook/internal/metrics"
)

// QueueDepthSource reports queue backlog. The queue owns its tables, so depth
// is read through this interface rather than by querying them from here;
// without a source configured the queue gauge is simply not reported.
type QueueDepthSource interface {
	Depth(ctx context.Context) (int64, error)
}

// SetQueueDepthSource attaches the queue that reports its own backlog depth.
func (s *Store) SetQueueDepthSource(source QueueDepthSource) {
	s.queueDepth = source
}

const queueDepthLogInterval = time.Minute

// MetricsSnapshot reads bounded-cardinality operational backlog counts.
func (s *Store) MetricsSnapshot(ctx context.Context) (metrics.Snapshot, error) {
	var snapshot metrics.Snapshot
	if s.queueDepth != nil {
		depth, err := s.queueDepth.Depth(ctx)
		if err != nil {
			// The outbox and dead gauges remain valid, so serve them rather
			// than failing the whole snapshot; log at a bounded rate.
			s.logQueueDepthError(err)
		} else {
			snapshot.QueueDepth = depth
		}
	}
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM mxhook_deliveries WHERE state IN ('pending', 'processing')`,
	).Scan(&snapshot.OutboxDepth); err != nil {
		return metrics.Snapshot{}, fmt.Errorf("state: outbox metrics: %w", err)
	}
	if err := s.db.QueryRowContext(ctx,
		`SELECT (SELECT COUNT(*) FROM mxhook_deliveries WHERE state = 'dead') +
		        (SELECT COUNT(*) FROM mxhook_ingress_failures)`,
	).Scan(&snapshot.DeadDepth); err != nil {
		return metrics.Snapshot{}, fmt.Errorf("state: dead metrics: %w", err)
	}
	return snapshot, nil
}

func (s *Store) logQueueDepthError(err error) {
	s.queueDepthLogMu.Lock()
	defer s.queueDepthLogMu.Unlock()
	now := s.now()
	if !s.queueDepthLoggedAt.IsZero() && now.Sub(s.queueDepthLoggedAt) < queueDepthLogInterval {
		return
	}
	s.queueDepthLoggedAt = now
	slog.Warn("state: queue depth unavailable", "err", err)
}

var _ metrics.Snapshotter = (*Store)(nil)
