package api_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mxhook/mxhook/api"
)

func TestAPIKey_RequiredWhenConfigured(t *testing.T) {
	srv := api.NewServer(api.Config{APIKey: "secret123"})

	// Without key — should get 401
	req := httptest.NewRequest(http.MethodGet, "/messages", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 without key, got %d", w.Code)
	}

	// With correct key — should get 200
	req2 := httptest.NewRequest(http.MethodGet, "/messages", nil)
	req2.Header.Set("X-API-Key", "secret123")
	w2 := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Errorf("expected 200 with key, got %d", w2.Code)
	}
}

func TestAPIKey_NotRequiredWhenEmpty(t *testing.T) {
	srv := api.NewServer(api.Config{}) // no APIKey set

	req := httptest.NewRequest(http.MethodGet, "/messages", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 without configured key, got %d", w.Code)
	}
}

func TestAPIKey_RejectsWrongKey(t *testing.T) {
	srv := api.NewServer(api.Config{APIKey: "correct"})

	req := httptest.NewRequest(http.MethodGet, "/messages", nil)
	req.Header.Set("X-API-Key", "wrong")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 with wrong key, got %d", w.Code)
	}
}
