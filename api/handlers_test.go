package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mxhook/mxhook/api"
)

func TestDomainCreate_ReturnsCreated(t *testing.T) {
	srv := api.NewServer(api.Config{})
	body := bytes.NewBufferString(`{"name":"example.com"}`)
	req := httptest.NewRequest(http.MethodPost, "/domains", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("got %d, want 201", w.Code)
	}
	var d api.Domain
	if err := json.NewDecoder(w.Body).Decode(&d); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if d.Name != "example.com" {
		t.Errorf("got name %q, want example.com", d.Name)
	}
	if d.ID == "" {
		t.Error("expected non-empty ID")
	}
}

func TestRouteCreate_ReturnsCreated(t *testing.T) {
	srv := api.NewServer(api.Config{})
	body := bytes.NewBufferString(`{"domain":"example.com","recipient_pattern":"*@example.com","webhook_url":"https://x.com/hook"}`)
	req := httptest.NewRequest(http.MethodPost, "/routes", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("got %d, want 201", w.Code)
	}
	var r api.Route
	if err := json.NewDecoder(w.Body).Decode(&r); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if r.WebhookURL != "https://x.com/hook" {
		t.Errorf("got webhook %q", r.WebhookURL)
	}
}

func TestDomainsList_ReturnsArray(t *testing.T) {
	srv := api.NewServer(api.Config{})
	req := httptest.NewRequest(http.MethodGet, "/domains", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", w.Code)
	}
	var domains []api.Domain
	json.NewDecoder(w.Body).Decode(&domains) //nolint:errcheck
	// empty array is fine
}

func TestDomainDelete_NotFound(t *testing.T) {
	srv := api.NewServer(api.Config{})
	req := httptest.NewRequest(http.MethodDelete, "/domains/nonexistent", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("got %d, want 404", w.Code)
	}
}

func TestDomainDelete_Existing(t *testing.T) {
	srv := api.NewServer(api.Config{})

	// Create domain first
	body := bytes.NewBufferString(`{"name":"delete-me.com"}`)
	req := httptest.NewRequest(http.MethodPost, "/domains", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create got %d, want 201", w.Code)
	}
	var d api.Domain
	json.NewDecoder(w.Body).Decode(&d) //nolint:errcheck

	// Delete it
	req2 := httptest.NewRequest(http.MethodDelete, "/domains/"+d.ID, nil)
	w2 := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w2, req2)
	if w2.Code != http.StatusNoContent {
		t.Fatalf("delete got %d, want 204", w2.Code)
	}
}

func TestRoutesList_ReturnsArray(t *testing.T) {
	srv := api.NewServer(api.Config{})
	req := httptest.NewRequest(http.MethodGet, "/routes", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", w.Code)
	}
	var routes []api.Route
	json.NewDecoder(w.Body).Decode(&routes) //nolint:errcheck
}
