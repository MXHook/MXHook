package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/mxhook/mxhook/types"
)

// Endpoint is a webhook delivery target.
type Endpoint struct {
	ID     string
	URL    string
	Secret string
}

// Config controls delivery behavior.
type Config struct {
	MaxAttempts         int
	TimeoutSecs         int
	MaxResponseBytes    int64
	AllowHTTP           bool
	AllowPrivateNetwork bool
}

// Transformer allows mutating the webhook payload before delivery (used by EE).
type Transformer interface {
	Transform(event *types.EmailEvent) ([]byte, error)
}

// Deliverer sends EmailEvents to webhook endpoints.
type Deliverer struct {
	cfg         Config
	client      *http.Client
	transformer Transformer
	policy      targetPolicy
}

// WithTransformer attaches a payload transformer (e.g. EE pipeline) to the Deliverer.
func WithTransformer(t Transformer) func(*Deliverer) {
	return func(d *Deliverer) { d.transformer = t }
}

// NewDeliverer creates a Deliverer.
func NewDeliverer(cfg Config, opts ...func(*Deliverer)) *Deliverer {
	if cfg.TimeoutSecs <= 0 {
		cfg.TimeoutSecs = 30
	}
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = 3
	}
	if cfg.MaxResponseBytes <= 0 {
		cfg.MaxResponseBytes = 64 * 1024
	}
	policy := targetPolicy{allowHTTP: cfg.AllowHTTP, allowPrivate: cfg.AllowPrivateNetwork}
	client := &http.Client{
		Timeout:   time.Duration(cfg.TimeoutSecs) * time.Second,
		Transport: policy.transport(),
	}
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("webhook: too many redirects")
		}
		return policy.validateURL(req.Context(), req.URL)
	}
	d := &Deliverer{
		cfg:    cfg,
		client: client,
		policy: policy,
	}
	for _, o := range opts {
		o(d)
	}
	return d
}

// Timeout returns the configured HTTP timeout.
func (d *Deliverer) Timeout() time.Duration {
	return time.Duration(d.cfg.TimeoutSecs) * time.Second
}

// ValidateEndpoint validates signing and network policy without sending data.
func (d *Deliverer) ValidateEndpoint(ctx context.Context, endpoint Endpoint) error {
	if endpoint.Secret == "" {
		return errors.New("webhook: signing secret is required")
	}
	return d.policy.validateRawURL(ctx, endpoint.URL)
}

// Deliver sends event to endpoint with HMAC signing and exponential backoff retry.
func (d *Deliverer) Deliver(ctx context.Context, ep Endpoint, event *types.EmailEvent) error {
	if err := d.ValidateEndpoint(ctx, ep); err != nil {
		return err
	}
	var (
		payload []byte
		err     error
	)
	if d.transformer != nil {
		payload, err = d.transformer.Transform(event)
	} else {
		payload, err = json.Marshal(event)
	}
	if err != nil {
		return fmt.Errorf("webhook: marshal: %w", err)
	}

	var lastErr error
	for attempt := 1; attempt <= d.cfg.MaxAttempts; attempt++ {
		// Sign per attempt so retries carry a fresh timestamp: receivers
		// enforce the tolerance window against replay.
		ts := time.Now()
		sig := SignTimestamped(payload, ep.Secret, ts)
		if err := d.post(ctx, ep, sig, strconv.FormatInt(ts.Unix(), 10), payload, event, attempt); err != nil {
			lastErr = err
			var deliveryErr *deliveryError
			if errors.As(err, &deliveryErr) && !deliveryErr.temporary {
				return err
			}
			if attempt < d.cfg.MaxAttempts {
				wait := time.Duration(attempt*attempt) * time.Second
				select {
				case <-time.After(wait):
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			continue
		}
		return nil
	}
	return fmt.Errorf("webhook: all %d attempts failed: %w", d.cfg.MaxAttempts, lastErr)
}

func (d *Deliverer) post(ctx context.Context, endpoint Endpoint, sig, timestamp string, payload []byte, event *types.EmailEvent, attempt int) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.URL, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("webhook: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-MXHook-Signature", sig)
	req.Header.Set("X-MXHook-Timestamp", timestamp)
	req.Header.Set("X-MXHook-Attempt", fmt.Sprintf("%d", attempt))
	if endpoint.ID != "" {
		req.Header.Set("X-MXHook-Delivery-ID", endpoint.ID)
	}
	req.Header.Set("X-MXHook-Event-ID", event.ID)
	req.Header.Set("X-MXHook-Trace-ID", event.TraceID)

	resp, err := d.client.Do(req)
	if err != nil {
		return &deliveryError{err: err, temporary: true}
	}
	_, copyErr := io.CopyN(io.Discard, resp.Body, d.cfg.MaxResponseBytes+1)
	closeErr := resp.Body.Close()
	if copyErr != nil && !errors.Is(copyErr, io.EOF) {
		return &deliveryError{err: fmt.Errorf("webhook: read response: %w", copyErr), temporary: true}
	}
	if closeErr != nil {
		return &deliveryError{err: fmt.Errorf("webhook: close response: %w", closeErr), temporary: true}
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		temporary := resp.StatusCode == http.StatusRequestTimeout || resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500
		return &deliveryError{err: fmt.Errorf("webhook: server returned %d", resp.StatusCode), temporary: temporary}
	}
	return nil
}

type deliveryError struct {
	err       error
	temporary bool
}

func (e *deliveryError) Error() string { return e.err.Error() }
func (e *deliveryError) Unwrap() error { return e.err }

// IsTemporary reports whether a failed delivery is safe to retry.
func IsTemporary(err error) bool {
	var deliveryErr *deliveryError
	return errors.As(err, &deliveryErr) && deliveryErr.temporary
}
