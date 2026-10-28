package runtime

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"time"

	"github.com/mxhook/mxhook/internal/metrics"
	"github.com/mxhook/mxhook/internal/state"
	"github.com/mxhook/mxhook/webhook"
)

type DeliveryStore interface {
	ClaimDelivery(ctx context.Context) (state.Delivery, error)
	ExtendDelivery(ctx context.Context, delivery state.Delivery, extension time.Duration) (time.Time, error)
	CompleteDelivery(ctx context.Context, delivery state.Delivery) error
	RetryDelivery(ctx context.Context, delivery state.Delivery, reason string, next time.Time) error
	DeadDelivery(ctx context.Context, delivery state.Delivery, reason string) error
}

type DeliveryWorkerConfig struct {
	MaxAttempts    int
	LeaseExtension time.Duration
	BaseRetry      time.Duration
	MaxRetry       time.Duration
}

type DeliveryWorker struct {
	store     DeliveryStore
	deliverer Deliverer
	cfg       DeliveryWorkerConfig
	backoff   func(consecutive int) time.Duration
}

func NewDeliveryWorker(store DeliveryStore, deliverer Deliverer, cfg DeliveryWorkerConfig) (*DeliveryWorker, error) {
	if store == nil || deliverer == nil {
		return nil, errors.New("runtime: delivery store and deliverer are required")
	}
	if cfg.MaxAttempts == 0 {
		cfg.MaxAttempts = 8
	}
	if cfg.LeaseExtension == 0 {
		cfg.LeaseExtension = 30 * time.Second
	}
	if cfg.BaseRetry == 0 {
		cfg.BaseRetry = 5 * time.Second
	}
	if cfg.MaxRetry == 0 {
		cfg.MaxRetry = time.Hour
	}
	if cfg.MaxAttempts < 1 || cfg.LeaseExtension <= 0 || cfg.BaseRetry <= 0 || cfg.MaxRetry < cfg.BaseRetry {
		return nil, errors.New("runtime: invalid delivery retry or lease configuration")
	}
	return &DeliveryWorker{store: store, deliverer: deliverer, cfg: cfg, backoff: workerBackoff}, nil
}

func (w *DeliveryWorker) Run(ctx context.Context) error {
	consecutive := 0
	for {
		delivery, err := w.store.ClaimDelivery(ctx)
		if err != nil {
			if claimInterruptedByShutdown(ctx, err) {
				return nil
			}
			consecutive++
			if consecutive >= maxConsecutiveWorkerFailures {
				return fmt.Errorf("runtime: claim webhook delivery: %w", err)
			}
			slog.Warn("runtime: webhook delivery claim failed; backing off", "attempt", consecutive, "err", err)
			if !sleepContext(ctx, w.backoff(consecutive)) {
				return nil
			}
			continue
		}
		if err := w.process(ctx, delivery); err != nil {
			if errors.Is(err, state.ErrLeaseLost) {
				// Another worker owns the row now, so it will be delivered
				// again. Whatever this attempt already did to the endpoint is
				// about to be repeated: never swallow this silently.
				metrics.WebhookDeliveries.WithLabelValues(metrics.OutcomeLeaseLost).Inc()
				slog.Warn("runtime: webhook delivery lease lost before its outcome was recorded; the endpoint may receive a duplicate",
					"delivery_id", delivery.ID, "attempt", delivery.Attempt, "err", err)
				continue
			}
			if errors.Is(err, context.Canceled) {
				continue
			}
			consecutive++
			if consecutive >= maxConsecutiveWorkerFailures {
				return err
			}
			slog.Warn("runtime: webhook delivery finish failed; backing off",
				"delivery_id", delivery.ID, "attempt", consecutive, "err", err)
			if !sleepContext(ctx, w.backoff(consecutive)) {
				return nil
			}
			continue
		}
		consecutive = 0
	}
}

func (w *DeliveryWorker) process(ctx context.Context, delivery state.Delivery) error {
	processCtx, cancel := context.WithCancel(ctx)
	leaseResult := make(chan error, 1)
	go w.renewLease(processCtx, cancel, delivery, leaseResult)

	err := w.deliverer.Deliver(processCtx, webhook.Endpoint{
		ID: delivery.ID, URL: delivery.WebhookURL, Secret: delivery.Secret,
	}, delivery.Event)
	cancel()
	if leaseErr := <-leaseResult; leaseErr != nil {
		return leaseErr
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err == nil {
		if err := w.store.CompleteDelivery(ctx, delivery); err != nil {
			return fmt.Errorf("runtime: complete webhook delivery %s: %w", delivery.ID, err)
		}
		metrics.WebhookDeliveries.WithLabelValues(metrics.OutcomeSuccess).Inc()
		if !delivery.Event.ReceivedAt.IsZero() {
			metrics.DeliveryLatency.Observe(time.Since(delivery.Event.ReceivedAt).Seconds())
		}
		return nil
	}
	if !webhook.IsTemporary(err) || delivery.Attempt >= w.cfg.MaxAttempts {
		if finishErr := w.store.DeadDelivery(ctx, delivery, err.Error()); finishErr != nil {
			return fmt.Errorf("runtime: mark webhook delivery %s dead: %w", delivery.ID, finishErr)
		}
		metrics.WebhookDeliveries.WithLabelValues(metrics.OutcomeDead).Inc()
		return nil
	}
	next := time.Now().Add(w.retryDelay(delivery.Attempt))
	if finishErr := w.store.RetryDelivery(ctx, delivery, err.Error(), next); finishErr != nil {
		return fmt.Errorf("runtime: schedule webhook delivery %s retry: %w", delivery.ID, finishErr)
	}
	metrics.WebhookDeliveries.WithLabelValues(metrics.OutcomeRetry).Inc()
	metrics.WebhookRetries.Inc()
	return nil
}

func (w *DeliveryWorker) renewLease(ctx context.Context, cancel context.CancelFunc, delivery state.Delivery, result chan<- error) {
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
		extended, err := w.store.ExtendDelivery(ctx, delivery, w.cfg.LeaseExtension)
		if err != nil {
			if ctx.Err() != nil {
				// Cancellation interrupted the extend: the delivery outcome
				// decides completion, not the aborted renewal.
				result <- nil
				return
			}
			cancel()
			result <- fmt.Errorf("runtime: extend webhook delivery %s: %w", delivery.ID, err)
			return
		}
		deadline = extended
	}
}

func (w *DeliveryWorker) retryDelay(attempt int) time.Duration {
	delay := w.cfg.BaseRetry
	for i := 1; i < attempt && delay < w.cfg.MaxRetry/2; i++ {
		delay *= 2
	}
	if delay > w.cfg.MaxRetry {
		delay = w.cfg.MaxRetry
	}
	// Full jitter in [80%, 120%], reducing synchronized retry storms.
	spread := delay / 5
	if spread <= 0 {
		return delay
	}
	random, err := rand.Int(rand.Reader, big.NewInt(int64(2*spread)+1))
	if err != nil {
		return delay
	}
	return delay - spread + time.Duration(random.Int64())
}
