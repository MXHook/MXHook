package webhook

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
)

// Replayer re-delivers messages from the DLQ.
type Replayer struct {
	dlq       DLQ
	deliverer *Deliverer
}

// NewReplayer creates a Replayer.
func NewReplayer(dlq DLQ, d *Deliverer) *Replayer {
	return &Replayer{dlq: dlq, deliverer: d}
}

// Replay re-delivers a DLQ message by ID to its original endpoint.
// If overrideURL is non-empty, it is used instead of the stored WebhookURL.
func (r *Replayer) Replay(ctx context.Context, messageID, overrideURL string) error {
	entry, err := r.dlq.Get(ctx, messageID)
	if err != nil {
		return fmt.Errorf("replay: %w", err)
	}

	url := entry.WebhookURL
	if overrideURL != "" {
		url = overrideURL
	}

	if err := r.rawPost(ctx, url, entry.Payload); err != nil {
		return fmt.Errorf("replay: deliver: %w", err)
	}

	return r.dlq.Remove(ctx, messageID)
}

func (r *Replayer) rawPost(ctx context.Context, url string, payload []byte) error {
	client := &http.Client{Timeout: r.deliverer.Timeout()}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("replay: server returned %d", resp.StatusCode)
	}
	return nil
}
