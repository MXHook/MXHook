package runtime

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/mxhook/mxhook/queue"
)

type MessageProcessor interface {
	Process(ctx context.Context, message queue.Message) error
}

type WorkerConfig struct {
	MaxAttempts    int
	LeaseExtension time.Duration
	FailureSink    IngressFailureSink
}

type IngressFailureSink interface {
	RecordIngressFailure(ctx context.Context, message queue.Message, reason string) error
	ClearIngressFailure(ctx context.Context, messageID string) error
}

type Worker struct {
	queue     queue.Dequeuer
	processor MessageProcessor
	cfg       WorkerConfig
	backoff   func(consecutive int) time.Duration
}

// maxConsecutiveWorkerFailures bounds transient-error retries in worker run
// loops: a store/queue blip (SQLite busy spike, Postgres failover) is retried
// with backoff instead of killing the process, while a persistent failure
// still exits after this many consecutive attempts.
const maxConsecutiveWorkerFailures = 10

const (
	workerBackoffBase = 100 * time.Millisecond
	workerBackoffCap  = 5 * time.Second
)

func workerBackoff(consecutive int) time.Duration {
	delay := workerBackoffBase
	for i := 1; i < consecutive && delay < workerBackoffCap; i++ {
		delay *= 2
	}
	if delay > workerBackoffCap {
		delay = workerBackoffCap
	}
	return delay
}

// sleepContext waits for the given duration; it reports false when the
// context is cancelled first.
func sleepContext(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func NewWorker(dequeuer queue.Dequeuer, processor MessageProcessor, cfg WorkerConfig) (*Worker, error) {
	if dequeuer == nil || processor == nil {
		return nil, errors.New("runtime: dequeuer and processor are required")
	}
	if cfg.MaxAttempts == 0 {
		cfg.MaxAttempts = 5
	}
	if cfg.MaxAttempts < 1 {
		return nil, errors.New("runtime: max attempts must be positive")
	}
	if cfg.LeaseExtension == 0 {
		cfg.LeaseExtension = 30 * time.Second
	}
	if cfg.LeaseExtension <= 0 {
		return nil, errors.New("runtime: lease extension must be positive")
	}
	return &Worker{queue: dequeuer, processor: processor, cfg: cfg, backoff: workerBackoff}, nil
}

// claimInterruptedByShutdown reports whether a claim failed because the
// worker is stopping; the run loop then ends cleanly instead of failing.
func claimInterruptedByShutdown(ctx context.Context, err error) bool {
	return ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

func (w *Worker) Run(ctx context.Context) error {
	consecutive := 0
	for {
		delivery, err := w.queue.Claim(ctx)
		if err != nil {
			if claimInterruptedByShutdown(ctx, err) {
				return nil
			}
			consecutive++
			if consecutive >= maxConsecutiveWorkerFailures {
				return fmt.Errorf("runtime: claim: %w", err)
			}
			slog.Warn("runtime: claim failed; backing off", "attempt", consecutive, "err", err)
			if !sleepContext(ctx, w.backoff(consecutive)) {
				return nil
			}
			continue
		}
		if err := w.processDelivery(ctx, delivery); err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, queue.ErrLeaseLost) {
				continue
			}
			consecutive++
			if consecutive >= maxConsecutiveWorkerFailures {
				return err
			}
			slog.Warn("runtime: delivery finish failed; backing off",
				"message_id", delivery.Message.ID, "attempt", consecutive, "err", err)
			if !sleepContext(ctx, w.backoff(consecutive)) {
				return nil
			}
			continue
		}
		consecutive = 0
	}
}

func (w *Worker) processDelivery(ctx context.Context, delivery queue.Delivery) error {
	processCtx, cancel := context.WithCancel(ctx)
	leaseErr := make(chan error, 1)
	go w.renewLease(processCtx, cancel, delivery, leaseErr)

	processErr := w.processor.Process(processCtx, delivery.Message)
	cancel()
	renewErr := <-leaseErr
	if renewErr != nil {
		return renewErr
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if processErr == nil {
		if err := w.queue.Ack(ctx, delivery.Message.ID, delivery.Receipt); err != nil {
			return fmt.Errorf("runtime: ack %s: %w", delivery.Message.ID, err)
		}
		// A redelivery may follow a crash between a recorded ingress failure
		// and its Nack; the successful Ack supersedes the stale failure row.
		// First attempts cannot have a prior recorded failure, so the hot
		// path skips the cleanup.
		if w.cfg.FailureSink != nil && delivery.Attempt > 1 {
			if err := w.cfg.FailureSink.ClearIngressFailure(ctx, delivery.Message.ID); err != nil {
				slog.Warn("runtime: clear stale ingress failure",
					"message_id", delivery.Message.ID, "trace_id", delivery.Message.TraceID, "err", err)
			}
		}
		return nil
	}
	if IsPermanent(processErr) || delivery.Attempt >= w.cfg.MaxAttempts {
		if w.cfg.FailureSink != nil {
			if err := w.cfg.FailureSink.RecordIngressFailure(ctx, delivery.Message, processErr.Error()); err != nil {
				return fmt.Errorf("runtime: record terminal ingress failure %s: %w", delivery.Message.ID, err)
			}
		}
		if err := w.queue.Nack(ctx, delivery.Message.ID, delivery.Receipt, processErr.Error()); err != nil {
			return fmt.Errorf("runtime: nack %s: %w", delivery.Message.ID, err)
		}
		return nil
	}
	slog.Warn("runtime: delivery will retry after lease expiry",
		"message_id", delivery.Message.ID,
		"trace_id", delivery.Message.TraceID,
		"attempt", delivery.Attempt,
		"err", processErr,
	)
	return nil
}

func (w *Worker) renewLease(ctx context.Context, cancel context.CancelFunc, delivery queue.Delivery, result chan<- error) {
	deadline := delivery.LockedUntil
	for {
		wait := time.Until(deadline) / 2
		if wait <= 0 {
			wait = time.Millisecond
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			result <- nil
			return
		case <-timer.C:
		}
		extended, err := w.queue.Extend(ctx, delivery.Message.ID, delivery.Receipt, w.cfg.LeaseExtension)
		if err != nil {
			if ctx.Err() != nil {
				// Cancellation interrupted the extend: the processing outcome
				// decides acknowledgement, not the aborted renewal.
				result <- nil
				return
			}
			cancel()
			result <- fmt.Errorf("runtime: extend lease %s: %w", delivery.Message.ID, err)
			return
		}
		deadline = extended
	}
}
