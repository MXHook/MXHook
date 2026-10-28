// Package cloudflare provides a Cloudflare Queues publisher that
// implements queue.Enqueuer by POSTing to the CF Queue HTTP API.
package cloudflare

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/mxhook/mxhook/queue"
)

const (
	apiBase              = "https://api.cloudflare.com/client/v4"
	maxQueueMessageBytes = 128_000
)

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
func New(cfg Config) (*Publisher, error) {
	if strings.TrimSpace(cfg.AccountID) == "" || strings.TrimSpace(cfg.QueueID) == "" || strings.TrimSpace(cfg.Token) == "" {
		return nil, errors.New("cfqueue: account ID, queue ID, and token are required")
	}
	if strings.ContainsAny(cfg.AccountID+cfg.QueueID, "/?#") {
		return nil, errors.New("cfqueue: account ID and queue ID contain invalid URL characters")
	}
	return &Publisher{
		cfg:    cfg,
		client: &http.Client{Timeout: 10 * time.Second},
		url: fmt.Sprintf(
			"%s/accounts/%s/queues/%s/messages",
			apiBase, cfg.AccountID, cfg.QueueID,
		),
	}, nil
}

// apiMessage mirrors the CF Queue HTTP API body shape.
type apiMessage struct {
	Body        json.RawMessage `json:"body"`
	ContentType string          `json:"content_type"`
}

// Enqueue publishes msg to the Cloudflare Queue.
func (p *Publisher) Enqueue(ctx context.Context, msg queue.Message) error {
	encoded, err := queue.EncodeMessage(msg)
	if err != nil {
		return fmt.Errorf("cfqueue: validate message: %w", err)
	}
	payload := apiMessage{
		Body:        json.RawMessage(encoded),
		ContentType: "json",
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("cfqueue: marshal: %w", err)
	}
	if len(data) > maxQueueMessageBytes {
		return fmt.Errorf("cfqueue: encoded message is %d bytes; Cloudflare limit is %d", len(data), maxQueueMessageBytes)
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
	defer func() { _ = resp.Body.Close() }() // response body fully consumed or abandoned; close error not actionable

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return fmt.Errorf("cfqueue: read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if detail := bytes.TrimSpace(body); len(detail) > 0 {
			return fmt.Errorf("cfqueue: unexpected status %d: %s", resp.StatusCode, detail)
		}
		return fmt.Errorf("cfqueue: unexpected status %d", resp.StatusCode)
	}
	if len(bytes.TrimSpace(body)) != 0 {
		var result struct {
			Success bool `json:"success"`
			Errors  []struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			} `json:"errors"`
		}
		if err := json.Unmarshal(body, &result); err != nil {
			return fmt.Errorf("cfqueue: decode response: %w", err)
		}
		if !result.Success {
			return fmt.Errorf("cfqueue: API reported failure: %v", result.Errors)
		}
	}
	return nil
}
