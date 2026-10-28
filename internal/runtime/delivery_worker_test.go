package runtime

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/mxhook/mxhook/internal/state"
	"github.com/mxhook/mxhook/types"
	"github.com/mxhook/mxhook/webhook"
)

type canceledDeliveryStore struct {
	claimError error
}

func (s canceledDeliveryStore) ClaimDelivery(context.Context) (state.Delivery, error) {
	return state.Delivery{}, s.claimError
}

func (canceledDeliveryStore) ExtendDelivery(context.Context, state.Delivery, time.Duration) (time.Time, error) {
	return time.Time{}, nil
}

func (canceledDeliveryStore) CompleteDelivery(context.Context, state.Delivery) error { return nil }

func (canceledDeliveryStore) RetryDelivery(context.Context, state.Delivery, string, time.Time) error {
	return nil
}

func (canceledDeliveryStore) DeadDelivery(context.Context, state.Delivery, string) error { return nil }

type unusedDeliverer struct{}

func (unusedDeliverer) Deliver(context.Context, webhook.Endpoint, *types.EmailEvent) error {
	return nil
}

type blockingExtendDeliveryStore struct {
	mu            sync.Mutex
	completes     int
	extendStarted chan struct{}
	once          sync.Once
}

func (s *blockingExtendDeliveryStore) ClaimDelivery(context.Context) (state.Delivery, error) {
	return state.Delivery{}, context.Canceled
}

func (s *blockingExtendDeliveryStore) ExtendDelivery(ctx context.Context, _ state.Delivery, _ time.Duration) (time.Time, error) {
	s.once.Do(func() { close(s.extendStarted) })
	<-ctx.Done()
	return time.Time{}, ctx.Err()
}

func (s *blockingExtendDeliveryStore) CompleteDelivery(context.Context, state.Delivery) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.completes++
	return nil
}

func (s *blockingExtendDeliveryStore) RetryDelivery(context.Context, state.Delivery, string, time.Time) error {
	return nil
}

func (s *blockingExtendDeliveryStore) DeadDelivery(context.Context, state.Delivery, string) error {
	return nil
}

type waitingDeliverer struct{ started <-chan struct{} }

func (d waitingDeliverer) Deliver(ctx context.Context, _ webhook.Endpoint, _ *types.EmailEvent) error {
	select {
	case <-d.started:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestDeliveryWorkerCompletesWhenCancelInterruptsLeaseRenewalAfterSuccess(t *testing.T) {
	store := &blockingExtendDeliveryStore{extendStarted: make(chan struct{})}
	worker, err := NewDeliveryWorker(store, waitingDeliverer{started: store.extendStarted},
		DeliveryWorkerConfig{LeaseExtension: 100 * time.Millisecond})
	if err != nil {
		t.Fatalf("NewDeliveryWorker: %v", err)
	}
	delivery := state.Delivery{
		ID: "delivery-1", Event: &types.EmailEvent{},
		Receipt: "receipt", LockedUntil: time.Now().Add(20 * time.Millisecond),
	}
	if err := worker.process(context.Background(), delivery); err != nil {
		t.Fatalf("process after successful delivery: %v", err)
	}
	if store.completes != 1 {
		t.Fatalf("completes = %d, want 1: successful delivery must reach CompleteDelivery", store.completes)
	}
}

type funcDeliveryStore struct {
	claim    func() (state.Delivery, error)
	complete func() error
}

func (s *funcDeliveryStore) ClaimDelivery(context.Context) (state.Delivery, error) {
	return s.claim()
}

func (s *funcDeliveryStore) ExtendDelivery(_ context.Context, _ state.Delivery, extension time.Duration) (time.Time, error) {
	return time.Now().Add(extension), nil
}

func (s *funcDeliveryStore) CompleteDelivery(context.Context, state.Delivery) error {
	if s.complete != nil {
		return s.complete()
	}
	return nil
}

func (s *funcDeliveryStore) RetryDelivery(context.Context, state.Delivery, string, time.Time) error {
	return nil
}

func (s *funcDeliveryStore) DeadDelivery(context.Context, state.Delivery, string) error { return nil }

func testStateDelivery() state.Delivery {
	return state.Delivery{
		ID: "delivery-1", Event: &types.EmailEvent{},
		Receipt: "receipt", LockedUntil: time.Now().Add(time.Hour),
	}
}

func TestRetryDelayStaysWithinJitteredBounds(t *testing.T) {
	worker, err := NewDeliveryWorker(&funcDeliveryStore{}, unusedDeliverer{}, DeliveryWorkerConfig{
		MaxAttempts: 8, BaseRetry: 100 * time.Millisecond, MaxRetry: time.Second,
	})
	if err != nil {
		t.Fatalf("NewDeliveryWorker: %v", err)
	}
	for attempt := 1; attempt <= 10; attempt++ {
		for range 50 {
			delay := worker.retryDelay(attempt)
			if delay <= 0 {
				t.Fatalf("attempt %d: non-positive delay %s", attempt, delay)
			}
			// Jitter spans [80%, 120%] of the capped exponential backoff.
			if delay > worker.cfg.MaxRetry*6/5 {
				t.Fatalf("attempt %d: delay %s exceeds jittered cap", attempt, delay)
			}
			if delay < worker.cfg.BaseRetry*4/5 {
				t.Fatalf("attempt %d: delay %s below jittered base", attempt, delay)
			}
		}
	}
	// Backoff must grow with attempts before saturating at the cap.
	if worker.retryDelay(1) >= worker.cfg.MaxRetry {
		t.Fatal("first attempt already saturated the retry cap")
	}
}

func TestDeliveryWorkerRetriesTransientClaimErrors(t *testing.T) {
	claims := 0
	store := &funcDeliveryStore{claim: func() (state.Delivery, error) {
		claims++
		if claims <= 3 {
			return state.Delivery{}, errors.New("database is locked (5)")
		}
		return state.Delivery{}, context.Canceled
	}}
	worker, err := NewDeliveryWorker(store, unusedDeliverer{}, DeliveryWorkerConfig{})
	if err != nil {
		t.Fatalf("NewDeliveryWorker: %v", err)
	}
	worker.backoff = func(int) time.Duration { return time.Millisecond }
	if err := worker.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v (transient claim errors must not kill the worker)", err)
	}
	if claims != 4 {
		t.Fatalf("claims = %d, want 4 (three retried failures then clean shutdown)", claims)
	}
}

func TestDeliveryWorkerExitsAfterPersistentClaimFailures(t *testing.T) {
	claims := 0
	store := &funcDeliveryStore{claim: func() (state.Delivery, error) {
		claims++
		return state.Delivery{}, errors.New("disk I/O error")
	}}
	worker, err := NewDeliveryWorker(store, unusedDeliverer{}, DeliveryWorkerConfig{})
	if err != nil {
		t.Fatalf("NewDeliveryWorker: %v", err)
	}
	worker.backoff = func(int) time.Duration { return time.Millisecond }
	if err := worker.Run(context.Background()); err == nil {
		t.Fatal("Run must exit on persistent claim failure")
	}
	if claims != maxConsecutiveWorkerFailures {
		t.Fatalf("claims = %d, want %d before giving up", claims, maxConsecutiveWorkerFailures)
	}
}

func TestDeliveryWorkerRetriesTransientCompleteFailure(t *testing.T) {
	claims, completes := 0, 0
	store := &funcDeliveryStore{}
	store.claim = func() (state.Delivery, error) {
		claims++
		if claims <= 2 {
			return testStateDelivery(), nil
		}
		return state.Delivery{}, context.Canceled
	}
	store.complete = func() error {
		completes++
		if completes == 1 {
			return errors.New("database is locked (5)")
		}
		return nil
	}
	worker, err := NewDeliveryWorker(store, unusedDeliverer{}, DeliveryWorkerConfig{})
	if err != nil {
		t.Fatalf("NewDeliveryWorker: %v", err)
	}
	worker.backoff = func(int) time.Duration { return time.Millisecond }
	if err := worker.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v (one failed completion must not kill the worker)", err)
	}
	if completes != 2 {
		t.Fatalf("completes = %d, want 2 (failed completion retried on redelivery)", completes)
	}
}

func TestDeliveryWorkerTreatsBackendInterruptionAfterCancellationAsCleanShutdown(t *testing.T) {
	worker, err := NewDeliveryWorker(
		canceledDeliveryStore{claimError: errors.New("sqlite: interrupted (9)")},
		unusedDeliverer{}, DeliveryWorkerConfig{},
	)
	if err != nil {
		t.Fatalf("NewDeliveryWorker: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := worker.Run(ctx); err != nil {
		t.Fatalf("Run after cancellation: %v", err)
	}
}
