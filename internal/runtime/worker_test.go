package runtime

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/mxhook/mxhook/queue"
)

type fakeDequeuer struct {
	mu          sync.Mutex
	acks        int
	nacks       int
	exts        int
	reason      string
	claimError  error
	extendError error
}

func (f *fakeDequeuer) Claim(context.Context) (queue.Delivery, error) {
	if f.claimError != nil {
		return queue.Delivery{}, f.claimError
	}
	return queue.Delivery{}, context.Canceled
}

func (f *fakeDequeuer) Ack(context.Context, string, string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.acks++
	return nil
}

func (f *fakeDequeuer) Nack(_ context.Context, _, _, reason string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nacks++
	f.reason = reason
	return nil
}

func (f *fakeDequeuer) Extend(_ context.Context, _, _ string, extension time.Duration) (time.Time, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.exts++
	if f.extendError != nil {
		return time.Time{}, f.extendError
	}
	return time.Now().Add(extension), nil
}

type processorFunc func(context.Context, queue.Message) error

func (f processorFunc) Process(ctx context.Context, message queue.Message) error {
	return f(ctx, message)
}

type captureFailureSink struct {
	calls   int
	reason  string
	cleared []string
}

func (s *captureFailureSink) ClearIngressFailure(_ context.Context, messageID string) error {
	s.cleared = append(s.cleared, messageID)
	return nil
}

func TestWorkerTreatsBackendInterruptionAfterCancellationAsCleanShutdown(t *testing.T) {
	dequeuer := &fakeDequeuer{claimError: errors.New("sqlite: interrupted (9)")}
	worker, err := NewWorker(dequeuer, processorFunc(func(context.Context, queue.Message) error { return nil }), WorkerConfig{})
	if err != nil {
		t.Fatalf("NewWorker: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := worker.Run(ctx); err != nil {
		t.Fatalf("Run after cancellation: %v", err)
	}
}

func (s *captureFailureSink) RecordIngressFailure(_ context.Context, _ queue.Message, reason string) error {
	s.calls++
	s.reason = reason
	return nil
}

func TestWorkerAcknowledgesSuccessfulDelivery(t *testing.T) {
	dequeuer := &fakeDequeuer{}
	worker, err := NewWorker(dequeuer, processorFunc(func(context.Context, queue.Message) error { return nil }), WorkerConfig{})
	if err != nil {
		t.Fatalf("NewWorker: %v", err)
	}
	if err := worker.processDelivery(context.Background(), testDelivery(1, time.Second)); err != nil {
		t.Fatalf("processDelivery: %v", err)
	}
	if dequeuer.acks != 1 || dequeuer.nacks != 0 {
		t.Fatalf("acks/nacks = %d/%d, want 1/0", dequeuer.acks, dequeuer.nacks)
	}
}

func TestWorkerLeavesTransientFailureForLeaseRetry(t *testing.T) {
	dequeuer := &fakeDequeuer{}
	worker, err := NewWorker(dequeuer, processorFunc(func(context.Context, queue.Message) error {
		return errors.New("temporary outage")
	}), WorkerConfig{MaxAttempts: 3})
	if err != nil {
		t.Fatalf("NewWorker: %v", err)
	}
	if err := worker.processDelivery(context.Background(), testDelivery(1, time.Second)); err != nil {
		t.Fatalf("processDelivery: %v", err)
	}
	if dequeuer.acks != 0 || dequeuer.nacks != 0 {
		t.Fatalf("acks/nacks = %d/%d, want 0/0", dequeuer.acks, dequeuer.nacks)
	}
}

func TestWorkerNacksPermanentAndExhaustedFailures(t *testing.T) {
	for _, test := range []struct {
		name    string
		attempt int
		err     error
	}{
		{name: "permanent", attempt: 1, err: Permanent(errors.New("invalid message"))},
		{name: "exhausted", attempt: 3, err: errors.New("still unavailable")},
	} {
		t.Run(test.name, func(t *testing.T) {
			dequeuer := &fakeDequeuer{}
			failures := &captureFailureSink{}
			worker, err := NewWorker(dequeuer, processorFunc(func(context.Context, queue.Message) error { return test.err }), WorkerConfig{MaxAttempts: 3, FailureSink: failures})
			if err != nil {
				t.Fatalf("NewWorker: %v", err)
			}
			if err := worker.processDelivery(context.Background(), testDelivery(test.attempt, time.Second)); err != nil {
				t.Fatalf("processDelivery: %v", err)
			}
			if dequeuer.acks != 0 || dequeuer.nacks != 1 || dequeuer.reason == "" {
				t.Fatalf("acks/nacks/reason = %d/%d/%q", dequeuer.acks, dequeuer.nacks, dequeuer.reason)
			}
			if failures.calls != 1 || failures.reason == "" {
				t.Fatalf("terminal failure sink = %d/%q", failures.calls, failures.reason)
			}
		})
	}
}

func TestWorkerExtendsLeaseDuringSlowProcessing(t *testing.T) {
	dequeuer := &fakeDequeuer{}
	worker, err := NewWorker(dequeuer, processorFunc(func(context.Context, queue.Message) error {
		time.Sleep(60 * time.Millisecond)
		return nil
	}), WorkerConfig{LeaseExtension: 100 * time.Millisecond})
	if err != nil {
		t.Fatalf("NewWorker: %v", err)
	}
	if err := worker.processDelivery(context.Background(), testDelivery(1, 40*time.Millisecond)); err != nil {
		t.Fatalf("processDelivery: %v", err)
	}
	if dequeuer.exts == 0 || dequeuer.acks != 1 {
		t.Fatalf("extensions/acks = %d/%d, want >=1/1", dequeuer.exts, dequeuer.acks)
	}
}

func TestWorkerCancelsProcessingWhenLeaseIsLost(t *testing.T) {
	dequeuer := &fakeDequeuer{extendError: queue.ErrLeaseLost}
	worker, err := NewWorker(dequeuer, processorFunc(func(ctx context.Context, _ queue.Message) error {
		<-ctx.Done()
		return ctx.Err()
	}), WorkerConfig{LeaseExtension: 100 * time.Millisecond})
	if err != nil {
		t.Fatalf("NewWorker: %v", err)
	}
	err = worker.processDelivery(context.Background(), testDelivery(1, 20*time.Millisecond))
	if !errors.Is(err, queue.ErrLeaseLost) {
		t.Fatalf("processDelivery error = %v, want ErrLeaseLost", err)
	}
	if dequeuer.acks != 0 || dequeuer.nacks != 0 {
		t.Fatalf("acks/nacks = %d/%d, want 0/0", dequeuer.acks, dequeuer.nacks)
	}
}

type blockingExtendDequeuer struct {
	mu            sync.Mutex
	acks          int
	extendStarted chan struct{}
	once          sync.Once
}

func (d *blockingExtendDequeuer) Claim(context.Context) (queue.Delivery, error) {
	return queue.Delivery{}, context.Canceled
}

func (d *blockingExtendDequeuer) Ack(context.Context, string, string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.acks++
	return nil
}

func (d *blockingExtendDequeuer) Nack(context.Context, string, string, string) error { return nil }

func (d *blockingExtendDequeuer) Extend(ctx context.Context, _, _ string, _ time.Duration) (time.Time, error) {
	d.once.Do(func() { close(d.extendStarted) })
	<-ctx.Done()
	return time.Time{}, ctx.Err()
}

func TestWorkerAcksWhenCancelInterruptsLeaseRenewalAfterSuccess(t *testing.T) {
	dequeuer := &blockingExtendDequeuer{extendStarted: make(chan struct{})}
	worker, err := NewWorker(dequeuer, processorFunc(func(ctx context.Context, _ queue.Message) error {
		select {
		case <-dequeuer.extendStarted:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}), WorkerConfig{LeaseExtension: 100 * time.Millisecond})
	if err != nil {
		t.Fatalf("NewWorker: %v", err)
	}
	if err := worker.processDelivery(context.Background(), testDelivery(1, 20*time.Millisecond)); err != nil {
		t.Fatalf("processDelivery after successful processing: %v", err)
	}
	if dequeuer.acks != 1 {
		t.Fatalf("acks = %d, want 1: successful processing must reach Ack", dequeuer.acks)
	}
}

func TestPermanentUnwrapsToUnderlyingError(t *testing.T) {
	sentinel := errors.New("underlying cause")
	wrapped := Permanent(fmt.Errorf("runtime: parse: %w", sentinel))
	if !IsPermanent(wrapped) {
		t.Fatal("IsPermanent must still recognize a permanent error")
	}
	if !errors.Is(wrapped, sentinel) {
		t.Fatal("errors.Is must see through Permanent to the wrapped cause")
	}
}

func TestWorkerClearsIngressFailureOnRedeliveredAck(t *testing.T) {
	claims := 0
	dequeuer := &funcDequeuer{claim: func() (queue.Delivery, error) {
		claims++
		if claims == 1 {
			return testDelivery(2, time.Hour), nil
		}
		return queue.Delivery{}, context.Canceled
	}}
	failures := &captureFailureSink{}
	worker, err := NewWorker(dequeuer, processorFunc(func(context.Context, queue.Message) error { return nil }),
		WorkerConfig{FailureSink: failures})
	if err != nil {
		t.Fatalf("NewWorker: %v", err)
	}
	if err := worker.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(failures.cleared) != 1 {
		t.Fatalf("cleared = %v, want the redelivered message's stale ingress failure cleared", failures.cleared)
	}
}

type funcDequeuer struct {
	claim func() (queue.Delivery, error)
	ack   func() error
}

func (d *funcDequeuer) Claim(context.Context) (queue.Delivery, error) { return d.claim() }

func (d *funcDequeuer) Ack(context.Context, string, string) error {
	if d.ack != nil {
		return d.ack()
	}
	return nil
}

func (d *funcDequeuer) Nack(context.Context, string, string, string) error { return nil }

func (d *funcDequeuer) Extend(_ context.Context, _, _ string, extension time.Duration) (time.Time, error) {
	return time.Now().Add(extension), nil
}

func TestWorkerRetriesTransientClaimErrors(t *testing.T) {
	claims := 0
	dequeuer := &funcDequeuer{claim: func() (queue.Delivery, error) {
		claims++
		if claims <= 3 {
			return queue.Delivery{}, errors.New("database is locked (5)")
		}
		return queue.Delivery{}, context.Canceled
	}}
	worker, err := NewWorker(dequeuer, processorFunc(func(context.Context, queue.Message) error { return nil }), WorkerConfig{})
	if err != nil {
		t.Fatalf("NewWorker: %v", err)
	}
	worker.backoff = func(int) time.Duration { return time.Millisecond }
	if err := worker.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v (transient claim errors must not kill the worker)", err)
	}
	if claims != 4 {
		t.Fatalf("claims = %d, want 4 (three retried failures then clean shutdown)", claims)
	}
}

func TestWorkerExitsAfterPersistentClaimFailures(t *testing.T) {
	claims := 0
	dequeuer := &funcDequeuer{claim: func() (queue.Delivery, error) {
		claims++
		return queue.Delivery{}, errors.New("disk I/O error")
	}}
	worker, err := NewWorker(dequeuer, processorFunc(func(context.Context, queue.Message) error { return nil }), WorkerConfig{})
	if err != nil {
		t.Fatalf("NewWorker: %v", err)
	}
	worker.backoff = func(int) time.Duration { return time.Millisecond }
	if err := worker.Run(context.Background()); err == nil {
		t.Fatal("Run must exit on persistent claim failure")
	}
	if claims != maxConsecutiveWorkerFailures {
		t.Fatalf("claims = %d, want %d before giving up", claims, maxConsecutiveWorkerFailures)
	}
}

func TestWorkerRetriesTransientAckFailure(t *testing.T) {
	claims, acks := 0, 0
	dequeuer := &funcDequeuer{}
	dequeuer.claim = func() (queue.Delivery, error) {
		claims++
		if claims <= 2 {
			return testDelivery(1, time.Hour), nil
		}
		return queue.Delivery{}, context.Canceled
	}
	dequeuer.ack = func() error {
		acks++
		if acks == 1 {
			return errors.New("database is locked (5)")
		}
		return nil
	}
	worker, err := NewWorker(dequeuer, processorFunc(func(context.Context, queue.Message) error { return nil }), WorkerConfig{})
	if err != nil {
		t.Fatalf("NewWorker: %v", err)
	}
	worker.backoff = func(int) time.Duration { return time.Millisecond }
	if err := worker.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v (one failed ack must not kill the worker)", err)
	}
	if acks != 2 {
		t.Fatalf("acks = %d, want 2 (failed ack retried on redelivery)", acks)
	}
}

func testDelivery(attempt int, lease time.Duration) queue.Delivery {
	return queue.Delivery{
		Message: queue.NewMessage(
			"runtime-test", "test", "", "sender@example.com",
			[]string{"inbox@example.com"}, time.Unix(1, 0), []byte("Subject: test\r\n\r\nbody"),
		),
		Receipt:     "receipt",
		LockedUntil: time.Now().Add(lease),
		Attempt:     attempt,
	}
}
