package webhook

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"time"
)

// Config holds delivery configuration.
type Config struct {
	MaxAttempts int
	TimeoutSecs int
}

// Deliverer sends webhook payloads to remote endpoints.
type Deliverer struct {
	cfg    Config
	client *http.Client
}

// NewDeliverer creates a Deliverer with the given config.
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

// Deliver POSTs payload to url, retrying up to MaxAttempts.
func (d *Deliverer) Deliver(ctx context.Context, url string, payload []byte) error {
	var lastErr error
	for i := range d.cfg.MaxAttempts {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := d.client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return nil
		}
		lastErr = fmt.Errorf("deliverer: attempt %d: server returned %d", i+1, resp.StatusCode)
	}
	return lastErr
}
