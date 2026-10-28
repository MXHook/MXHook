package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mxhook/mxhook/api"
	"github.com/mxhook/mxhook/webhook"
)

// --- OpenAPI spec endpoint ---

func TestOpenAPISpec_Returns200WithYAML(t *testing.T) {
	srv := newTestServer()
	req := authed(httptest.NewRequest(http.MethodGet, "/openapi.yaml", nil))
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", w.Code)
	}
	ct := w.Header().Get("Content-Type")
	if ct != "application/yaml" {
		t.Errorf("got Content-Type %q, want application/yaml", ct)
	}
	if len(w.Body.Bytes()) == 0 {
		t.Error("expected non-empty body")
	}
}

// --- Domain create: invalid body ---

func TestDomainCreate_InvalidJSON_Returns400(t *testing.T) {
	srv := newTestServer()
	body := bytes.NewBufferString(`not-json`)
	req := authed(httptest.NewRequest(http.MethodPost, "/domains", body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400", w.Code)
	}
}

func TestDomainCreate_MissingName_Returns400(t *testing.T) {
	srv := newTestServer()
	body := bytes.NewBufferString(`{"name":""}`)
	req := authed(httptest.NewRequest(http.MethodPost, "/domains", body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400", w.Code)
	}
}

// --- Route create: invalid body ---

func TestRouteCreate_InvalidJSON_Returns400(t *testing.T) {
	srv := newTestServer()
	body := bytes.NewBufferString(`{bad json}`)
	req := authed(httptest.NewRequest(http.MethodPost, "/routes", body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400", w.Code)
	}
}

func TestRouteCreate_MissingWebhookURL_Returns400(t *testing.T) {
	srv := newTestServer()
	body := bytes.NewBufferString(`{"domain":"example.com","recipient_pattern":"*@example.com"}`)
	req := authed(httptest.NewRequest(http.MethodPost, "/routes", body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400", w.Code)
	}
}

// --- Route delete ---

func TestRouteDelete_NotFound_Returns404(t *testing.T) {
	srv := newTestServer()
	req := authed(httptest.NewRequest(http.MethodDelete, "/routes/nonexistent", nil))
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("got %d, want 404", w.Code)
	}
}

func TestRouteDelete_Existing_Returns204(t *testing.T) {
	srv := newTestServer()

	// Create route first
	body := bytes.NewBufferString(`{"domain":"example.com","recipient_pattern":"*@example.com","webhook_url":"https://x.com/hook"}`)
	req := authed(httptest.NewRequest(http.MethodPost, "/routes", body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create got %d, want 201", w.Code)
	}
	var r api.Route
	if err := json.NewDecoder(w.Body).Decode(&r); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// Delete it
	req2 := authed(httptest.NewRequest(http.MethodDelete, "/routes/"+r.ID, nil))
	w2 := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w2, req2)
	if w2.Code != http.StatusNoContent {
		t.Fatalf("delete got %d, want 204", w2.Code)
	}
}

// --- Messages ---

func TestGetMessage_Returns404(t *testing.T) {
	srv := newTestServer()
	req := authed(httptest.NewRequest(http.MethodGet, "/messages/some-id", nil))
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("got %d, want 404", w.Code)
	}
}

func TestListMessages_ReturnsEmptyArray(t *testing.T) {
	srv := newTestServer()
	req := authed(httptest.NewRequest(http.MethodGet, "/messages", nil))
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", w.Code)
	}
	var msgs []api.Message
	if err := json.NewDecoder(w.Body).Decode(&msgs); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(msgs) != 0 {
		t.Errorf("expected empty array, got %d items", len(msgs))
	}
}

// --- Replay ---

func TestReplay_ReturnsAccepted(t *testing.T) {
	srv := newTestServer()
	req := authed(httptest.NewRequest(http.MethodPost, "/replay/msg-1", nil))
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("got %d, want 202", w.Code)
	}
	var body map[string]string
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["status"] != "queued" {
		t.Errorf("got status %q, want queued", body["status"])
	}
}

// --- DLQ: nil dlq ---

func TestDLQList_NilDLQ_ReturnsEmptyArray(t *testing.T) {
	srv := newTestServer() // no DLQ attached
	req := authed(httptest.NewRequest(http.MethodGet, "/dlq", nil))
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", w.Code)
	}
	var entries []any
	if err := json.NewDecoder(w.Body).Decode(&entries); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected empty, got %d", len(entries))
	}
}

// --- DLQ: delete ---

func TestDLQDelete_NilDLQ_Returns503(t *testing.T) {
	srv := newTestServer()
	req := authed(httptest.NewRequest(http.MethodDelete, "/dlq/msg-1", nil))
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("got %d, want 503", w.Code)
	}
}

func TestDLQDelete_Existing_Returns204(t *testing.T) {
	dlq := webhook.NewInMemoryDLQ()
	if err := dlq.Add(context.Background(), webhook.DLQEntry{
		MessageID:  "msg-2",
		WebhookURL: "https://example.com/hook",
		FailedAt:   time.Now(),
		Reason:     "timeout",
		Payload:    []byte(`{}`),
	}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	srv := newTestServer(api.WithDLQ(dlq))
	req := authed(httptest.NewRequest(http.MethodDelete, "/dlq/msg-2", nil))
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("got %d, want 204", w.Code)
	}
}

// --- DLQ: List with error-returning DLQ ---

type errDLQ struct{}

func (e *errDLQ) Add(_ context.Context, _ webhook.DLQEntry) error { return nil }
func (e *errDLQ) List(_ context.Context) ([]webhook.DLQEntry, error) {
	return nil, errors.New("backend unavailable")
}

func (e *errDLQ) Get(_ context.Context, _ string) (webhook.DLQEntry, error) {
	return webhook.DLQEntry{}, errors.New("not found")
}

func (e *errDLQ) Remove(_ context.Context, _ string) error {
	return errors.New("remove failed")
}

func TestDLQList_BackendError_Returns500(t *testing.T) {
	srv := newTestServer(api.WithDLQ(&errDLQ{}))
	req := authed(httptest.NewRequest(http.MethodGet, "/dlq", nil))
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("got %d, want 500", w.Code)
	}
}

func TestDLQDelete_BackendError_Returns404(t *testing.T) {
	srv := newTestServer(api.WithDLQ(&errDLQ{}))
	req := authed(httptest.NewRequest(http.MethodDelete, "/dlq/msg-x", nil))
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("got %d, want 404", w.Code)
	}
}

// --- Store List coverage: ensure iteration path with items ---

func TestDomainList_AfterCreate_ReturnsItems(t *testing.T) {
	srv := newTestServer()

	// Create two domains
	for _, name := range []string{"a.com", "b.com"} {
		body := bytes.NewBufferString(`{"name":"` + name + `"}`)
		req := authed(httptest.NewRequest(http.MethodPost, "/domains", body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("create %s: got %d", name, w.Code)
		}
	}

	req := authed(httptest.NewRequest(http.MethodGet, "/domains", nil))
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list got %d", w.Code)
	}
	var domains []api.Domain
	if err := json.NewDecoder(w.Body).Decode(&domains); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(domains) != 2 {
		t.Errorf("got %d domains, want 2", len(domains))
	}
}

func TestRouteList_AfterCreate_ReturnsItems(t *testing.T) {
	srv := newTestServer()

	body := bytes.NewBufferString(`{"domain":"example.com","recipient_pattern":"*@example.com","webhook_url":"https://x.com/hook"}`)
	req := authed(httptest.NewRequest(http.MethodPost, "/routes", body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create got %d", w.Code)
	}

	req2 := authed(httptest.NewRequest(http.MethodGet, "/routes", nil))
	w2 := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("list got %d", w2.Code)
	}
	var routes []api.Route
	if err := json.NewDecoder(w2.Body).Decode(&routes); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(routes) != 1 {
		t.Errorf("got %d routes, want 1", len(routes))
	}
}
