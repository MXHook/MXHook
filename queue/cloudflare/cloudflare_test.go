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
	p := New(cfg)
	if p == nil {
		t.Fatal("New returned nil")
	}
}

func TestNew_EmptyConfig(t *testing.T) {
	// New() with empty config should still return a Publisher (no validation in New).
	p := New(Config{})
	if p == nil {
		t.Fatal("New returned nil for empty config")
	}
}

func TestNew_URLFormat(t *testing.T) {
	cfg := Config{AccountID: "myacct", QueueID: "myqueue", Token: "mytoken"}
	p := New(cfg)
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
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := Config{AccountID: "acct", QueueID: "q", Token: "my-token"}
	p := newTestPublisher(srv.URL, cfg)

	msg := queue.Message{
		ID:   "msg-001",
		Body: []byte("hello world"),
		Meta: map[string]string{"env": "prod"},
	}
	if err := p.Enqueue(context.Background(), msg); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	if capturedAuth != "Bearer my-token" {
		t.Errorf("Authorization = %q, want %q", capturedAuth, "Bearer my-token")
	}
	if capturedContentType != "application/json" {
		t.Errorf("Content-Type = %q, want %q", capturedContentType, "application/json")
	}

	var req apiRequest
	if err := json.Unmarshal(capturedBody, &req); err != nil {
		t.Fatalf("parse body: %v", err)
	}
	if len(req.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(req.Messages))
	}
	if req.Messages[0].Body != "hello world" {
		t.Errorf("body = %q, want %q", req.Messages[0].Body, "hello world")
	}
	if req.Messages[0].ContentType != "text/plain" {
		t.Errorf("content_type = %q, want %q", req.Messages[0].ContentType, "text/plain")
	}
	if req.Messages[0].Meta["message_id"] != "msg-001" {
		t.Errorf("meta.message_id = %q, want %q", req.Messages[0].Meta["message_id"], "msg-001")
	}
	if req.Messages[0].Meta["env"] != "prod" {
		t.Errorf("meta.env = %q, want %q", req.Messages[0].Meta["env"], "prod")
	}
}

// ---- HTTP error responses ----

func TestEnqueue_HTTP4xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	p := newTestPublisher(srv.URL, Config{AccountID: "a", QueueID: "q", Token: "bad-tok"})
	err := p.Enqueue(context.Background(), queue.Message{ID: "1", Body: []byte("x")})
	if err == nil {
		t.Fatal("expected error for 401 response")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("error should mention status 401, got: %v", err)
	}
}

func TestEnqueue_HTTP5xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	p := newTestPublisher(srv.URL, Config{AccountID: "a", QueueID: "q", Token: "tok"})
	err := p.Enqueue(context.Background(), queue.Message{ID: "1", Body: []byte("x")})
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
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(code)
			}))
			defer srv.Close()

			p := newTestPublisher(srv.URL, Config{AccountID: "a", QueueID: "q", Token: "tok"})
			err := p.Enqueue(context.Background(), queue.Message{ID: "1", Body: []byte("x")})
			if err != nil {
				t.Errorf("unexpected error for status %d: %v", code, err)
			}
		})
	}
}

// ---- Context cancellation ----

func TestEnqueue_ContextCancelled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Slow server — context should cancel before response
		time.Sleep(10 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	p := newTestPublisher(srv.URL, Config{AccountID: "a", QueueID: "q", Token: "tok"})
	p.client = &http.Client{Timeout: 30 * time.Second} // ensure client doesn't timeout first

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	err := p.Enqueue(ctx, queue.Message{ID: "1", Body: []byte("x")})
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

// ---- Network error ----

func TestEnqueue_NetworkError(t *testing.T) {
	// Use a server that immediately closes to simulate network error
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Close connection abruptly
		hj, ok := w.(http.Hijacker)
		if !ok {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		conn, _, _ := hj.Hijack()
		conn.Close()
	}))
	defer srv.Close()

	p := newTestPublisher(srv.URL, Config{AccountID: "a", QueueID: "q", Token: "tok"})
	err := p.Enqueue(context.Background(), queue.Message{ID: "1", Body: []byte("x")})
	if err == nil {
		t.Fatal("expected error for abrupt connection close")
	}
}

// ---- Meta handling ----

func TestBuildMeta_NilMeta(t *testing.T) {
	msg := queue.Message{ID: "xyz", Body: []byte("b"), Meta: nil}
	m := buildMeta(msg)
	if m["message_id"] != "xyz" {
		t.Errorf("message_id = %q, want %q", m["message_id"], "xyz")
	}
	if len(m) != 1 {
		t.Errorf("expected 1 key in meta, got %d", len(m))
	}
}

func TestBuildMeta_WithExistingMeta(t *testing.T) {
	msg := queue.Message{
		ID:   "abc",
		Body: []byte("b"),
		Meta: map[string]string{"source": "smtp", "tenant": "acme"},
	}
	m := buildMeta(msg)
	if m["message_id"] != "abc" {
		t.Errorf("message_id = %q, want %q", m["message_id"], "abc")
	}
	if m["source"] != "smtp" {
		t.Errorf("source = %q, want smtp", m["source"])
	}
	if m["tenant"] != "acme" {
		t.Errorf("tenant = %q, want acme", m["tenant"])
	}
}

func TestBuildMeta_DoesNotMutateOriginal(t *testing.T) {
	orig := map[string]string{"k": "v"}
	msg := queue.Message{ID: "id", Body: []byte("b"), Meta: orig}
	m := buildMeta(msg)
	m["injected"] = "yes"
	if _, ok := orig["injected"]; ok {
		t.Error("buildMeta mutated original Meta map")
	}
}

// ---- Implements Enqueuer interface ----

func TestPublisher_ImplementsEnqueuer(t *testing.T) {
	var _ interface {
		Enqueue(ctx context.Context, msg queue.Message) error
	} = (*Publisher)(nil)
}
