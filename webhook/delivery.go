package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/mxhook/mxhook/internal/types"
)

// Endpoint is a webhook delivery target.
type Endpoint struct {
	URL    string
	Secret string
}

// Config controls delivery behavior.
type Config struct {
	MaxAttempts int
	TimeoutSecs int
}

// Deliverer sends EmailEvents to webhook endpoints.
type Deliverer struct {
	cfg    Config
	client *http.Client
}

// NewDeliverer creates a Deliverer.
func NewDeliverer(cfg Config) *Deliverer {
	if cfg.TimeoutSecs <= 0 {
		cfg.TimeoutSecs = 30
	}
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = 3
	}
	return &Deliverer{
		cfg:    cfg,
		client: &http.Client{Timeout: time.Duration(cfg.TimeoutSecs) * time.Second},
	}
}

// Timeout returns the configured HTTP timeout.
func (d *Deliverer) Timeout() time.Duration {
	return time.Duration(d.cfg.TimeoutSecs) * time.Second
}

// Deliver sends event to endpoint with HMAC signing and exponential backoff retry.
func (d *Deliverer) Deliver(ctx context.Context, ep Endpoint, event *types.EmailEvent) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("webhook: marshal: %w", err)
	}
	sig := Sign(payload, ep.Secret)

	var lastErr error
	for attempt := 1; attempt <= d.cfg.MaxAttempts; attempt++ {
		if err := d.post(ctx, ep.URL, sig, payload); err != nil {
			lastErr = err
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

func (d *Deliverer) post(ctx context.Context, url, sig string, payload []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-MXHook-Signature", sig)

	resp, err := d.client.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook: server returned %d", resp.StatusCode)
	}
	return nil
}
