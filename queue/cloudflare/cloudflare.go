// Package cloudflare provides a Cloudflare Queues publisher that
// implements queue.Enqueuer by POSTing to the CF Queue HTTP API.
package cloudflare

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/mxhook/mxhook/queue"
)

const apiBase = "https://api.cloudflare.com/client/v4"

// Config holds Cloudflare Queue credentials.
type Config struct {
	AccountID string // CF_QUEUE_ACCOUNT_ID
	QueueID   string // CF_QUEUE_ID
	Token     string // CF_QUEUE_TOKEN
}

// Publisher publishes queue.Messages to a Cloudflare Queue.
type Publisher struct {
	cfg    Config
	client *http.Client
	url    string
}

// New creates a Publisher. cfg must have AccountID, QueueID and Token set.
func New(cfg Config) *Publisher {
	return &Publisher{
		cfg:    cfg,
		client: &http.Client{Timeout: 10 * time.Second},
		url: fmt.Sprintf(
			"%s/accounts/%s/queues/%s/messages",
			apiBase, cfg.AccountID, cfg.QueueID,
		),
	}
}

// apiMessage mirrors the CF Queue HTTP API body shape.
type apiMessage struct {
	Body        string            `json:"body"`
	ContentType string            `json:"content_type"`
	Meta        map[string]string `json:"meta,omitempty"`
}

type apiRequest struct {
	Messages []apiMessage `json:"messages"`
}

// Enqueue publishes msg to the Cloudflare Queue.
func (p *Publisher) Enqueue(ctx context.Context, msg queue.Message) error {
	payload := apiRequest{
		Messages: []apiMessage{
			{
				// CF Queues expect a string body; base64 via json.Marshal
				// is handled automatically for []byte — encode raw bytes
				// as a JSON string by converting to string here.
				Body:        string(msg.Body),
				ContentType: "text/plain",
				Meta:        buildMeta(msg),
			},
		},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("cfqueue: marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.url, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("cfqueue: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+p.cfg.Token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("cfqueue: http post: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("cfqueue: unexpected status %d", resp.StatusCode)
	}
	return nil
}

// buildMeta merges queue message ID into the meta map.
func buildMeta(msg queue.Message) map[string]string {
	m := make(map[string]string, len(msg.Meta)+1)
	for k, v := range msg.Meta {
		m[k] = v
	}
	m["message_id"] = msg.ID
	return m
}
