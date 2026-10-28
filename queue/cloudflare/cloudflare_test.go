package cloudflare

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mxhook/mxhook/queue"
)

// newTestPublisher builds a Publisher pointing at the given test server URL,
// without going through New() which hard-codes apiBase.
func newTestPublisher(srvURL string, cfg Config) *Publisher {
	return &Publisher{
		cfg:    cfg,
		client: &http.Client{Timeout: 5 * time.Second},
		url:    srvURL + "/accounts/" + cfg.AccountID + "/queues/" + cfg.QueueID + "/messages",
	}
}

// ---- New() ----

func TestNew_ReturnsPublisher(t *testing.T) {
	cfg := Config{AccountID: "acct", QueueID: "q", Token: "tok"}
	p, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if p == nil {
		t.Fatal("New returned nil")
	}
}

func TestNew_EmptyConfig(t *testing.T) {
	if _, err := New(Config{}); err == nil {
		t.Fatal("New accepted empty configuration")
	}
}

func TestNew_URLFormat(t *testing.T) {
	cfg := Config{AccountID: "myacct", QueueID: "myqueue", Token: "mytoken"}
	p, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	expected := "https://api.cloudflare.com/client/v4/accounts/myacct/queues/myqueue/messages"
	if p.url != expected {
		t.Errorf("url = %q, want %q", p.url, expected)
	}
}

// ---- Enqueue success ----

func TestEnqueue_Success(t *testing.T) {
	var capturedAuth string
	var capturedContentType string
	var capturedBody []byte

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		capturedAuth = r.Header.Get("Authorization")
		capturedContentType = r.Header.Get("Content-Type")
		capturedBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"errors":[]}`))
	}))
	defer srv.Close()

	cfg := Config{AccountID: "acct", QueueID: "q", Token: "my-token"}
	p := newTestPublisher(srv.URL, cfg)

	msg := cloudflareTestMessage("msg-001", []byte("hello world"))
	if err := p.Enqueue(context.Background(), msg); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	if capturedAuth != "Bearer my-token" {
		t.Errorf("Authorization = %q, want %q", capturedAuth, "Bearer my-token")
	}
	if capturedContentType != "application/json" {
		t.Errorf("Content-Type = %q, want %q", capturedContentType, "application/json")
	}

	var req apiMessage
	if err := json.Unmarshal(capturedBody, &req); err != nil {
		t.Fatalf("parse body: %v", err)
	}
	decoded, err := queue.DecodeMessage(req.Body)
	if err != nil {
		t.Fatalf("decode queue body: %v", err)
	}
	if decoded.ID != msg.ID || string(decoded.Body) != "hello world" {
		t.Errorf("decoded message = %#v", decoded)
	}
	if req.ContentType != "json" {
		t.Errorf("content_type = %q, want json", req.ContentType)
	}
}

// ---- HTTP error responses ----

func TestEnqueue_HTTP4xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	p := newTestPublisher(srv.URL, Config{AccountID: "a", QueueID: "q", Token: "bad-tok"})
	err := p.Enqueue(context.Background(), cloudflareTestMessage("1", []byte("x")))
	if err == nil {
		t.Fatal("expected error for 401 response")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("error should mention status 401, got: %v", err)
	}
}

func TestEnqueue_HTTP5xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	p := newTestPublisher(srv.URL, Config{AccountID: "a", QueueID: "q", Token: "tok"})
	err := p.Enqueue(context.Background(), cloudflareTestMessage("1", []byte("x")))
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error should mention status 500, got: %v", err)
	}
}

func TestEnqueue_HTTP200Range(t *testing.T) {
	for _, code := range []int{200, 201, 202, 204} {
		code := code
		t.Run(http.StatusText(code), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(code)
			}))
			defer srv.Close()

			p := newTestPublisher(srv.URL, Config{AccountID: "a", QueueID: "q", Token: "tok"})
			err := p.Enqueue(context.Background(), cloudflareTestMessage("1", []byte("x")))
			if err != nil {
				t.Errorf("unexpected error for status %d: %v", code, err)
			}
		})
	}
}

func TestEnqueueRejectsCloudflareSuccessFalse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"success":false,"errors":[{"code":1000,"message":"rejected"}]}`))
	}))
	defer srv.Close()
	p := newTestPublisher(srv.URL, Config{AccountID: "a", QueueID: "q", Token: "tok"})
	if err := p.Enqueue(context.Background(), cloudflareTestMessage("1", []byte("x"))); err == nil {
		t.Fatal("Enqueue accepted success=false API response")
	}
}

func TestEnqueueRejectsOversizedEncodedMessage(t *testing.T) {
	p := newTestPublisher("http://127.0.0.1", Config{AccountID: "a", QueueID: "q", Token: "tok"})
	if err := p.Enqueue(context.Background(), cloudflareTestMessage("large", make([]byte, maxQueueMessageBytes))); err == nil {
		t.Fatal("Enqueue accepted a message above Cloudflare's queue limit")
	}
}

// ---- Context cancellation ----

func TestEnqueue_ContextCancelled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Slow server — context should cancel before response
		time.Sleep(10 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	p := newTestPublisher(srv.URL, Config{AccountID: "a", QueueID: "q", Token: "tok"})
	p.client = &http.Client{Timeout: 30 * time.Second} // ensure client doesn't timeout first

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	err := p.Enqueue(ctx, cloudflareTestMessage("1", []byte("x")))
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

// ---- Network error ----

func TestEnqueue_NetworkError(t *testing.T) {
	// Use a server that immediately closes to simulate network error
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Close connection abruptly
		hj, ok := w.(http.Hijacker)
		if !ok {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		conn, _, _ := hj.Hijack()
		_ = conn.Close()
	}))
	defer srv.Close()

	p := newTestPublisher(srv.URL, Config{AccountID: "a", QueueID: "q", Token: "tok"})
	err := p.Enqueue(context.Background(), cloudflareTestMessage("1", []byte("x")))
	if err == nil {
		t.Fatal("expected error for abrupt connection close")
	}
}

func TestNew_RejectsURLDelimiterCharacters(t *testing.T) {
	for _, cfg := range []Config{
		{AccountID: "acct/../other", QueueID: "q", Token: "tok"},
		{AccountID: "acct?admin=1", QueueID: "q", Token: "tok"},
		{AccountID: "acct#frag", QueueID: "q", Token: "tok"},
		{AccountID: "acct", QueueID: "q/other", Token: "tok"},
		{AccountID: "acct", QueueID: "q?x=1", Token: "tok"},
		{AccountID: "acct", QueueID: "q#frag", Token: "tok"},
	} {
		if _, err := New(cfg); err == nil {
			t.Errorf("New accepted account %q queue %q", cfg.AccountID, cfg.QueueID)
		}
	}
}

func TestEnqueue_ErrorIncludesResponseBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"success":false,"errors":[{"code":11000,"message":"queue not found"}]}`))
	}))
	defer srv.Close()

	p := newTestPublisher(srv.URL, Config{AccountID: "a", QueueID: "q", Token: "tok"})
	err := p.Enqueue(context.Background(), cloudflareTestMessage("1", []byte("x")))
	if err == nil {
		t.Fatal("expected error for 400 response")
	}
	if !strings.Contains(err.Error(), "queue not found") {
		t.Errorf("error should include API response body, got: %v", err)
	}
}

// ---- Implements Enqueuer interface ----

func TestPublisher_ImplementsEnqueuer(_ *testing.T) {
	var _ interface {
		Enqueue(ctx context.Context, msg queue.Message) error
	} = (*Publisher)(nil)
}

func cloudflareTestMessage(id string, body []byte) queue.Message {
	return queue.NewMessage(
		id,
		"test",
		"",
		"sender@example.com",
		[]string{"inbox@example.com"},
		time.Date(2026, time.August, 28, 12, 0, 0, 0, time.UTC),
		body,
	)
}
