package api_test

import (
	"encoding/json"
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

// An unconfigured server has no way to authenticate anyone, so the control
// plane is refused outright rather than served to every caller.
func TestAPIKey_ControlPlaneClosedWhenEmpty(t *testing.T) {
	srv := api.NewServer(api.Config{}) // no APIKey set

	req := httptest.NewRequest(http.MethodGet, "/messages", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 without configured key, got %d", w.Code)
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

func TestAPIAuthErrorsUseJSONEnvelopeAndRequestID(t *testing.T) {
	srv := api.NewServer(api.Config{APIKey: "secret"})
	req := httptest.NewRequest(http.MethodGet, "/domains", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", w.Code)
	}
	if got := w.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("content type = %q", got)
	}
	if got := w.Header().Get("X-Request-ID"); got == "" {
		t.Fatal("missing X-Request-ID")
	}
	var body map[string]string
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil || body["error"] != "unauthorized" {
		t.Fatalf("body = %v, err=%v", body, err)
	}
}
