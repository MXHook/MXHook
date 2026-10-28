package api_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mxhook/mxhook/api"
)

type failingReadiness struct{}

func (failingReadiness) Ping(context.Context) error { return errors.New("database unavailable") }

func TestHealth_Returns200(t *testing.T) {
	srv := api.NewServer(api.Config{})
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("got %d, want 200", w.Code)
	}
	var body map[string]string
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("unexpected body: %v", body)
	}
}

func TestLivenessStaysUpWhenReadinessFails(t *testing.T) {
	server := api.NewServer(api.Config{}, api.WithReadinessChecker(failingReadiness{}))
	for path, want := range map[string]int{"/healthz": http.StatusOK, "/readyz": http.StatusServiceUnavailable} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		response := httptest.NewRecorder()
		server.Handler().ServeHTTP(response, request)
		if response.Code != want {
			t.Fatalf("%s status = %d, want %d", path, response.Code, want)
		}
	}
}

func TestMetricsHandlerIsExposed(t *testing.T) {
	server := api.NewServer(api.Config{}, api.WithMetricsHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("mxhook_test 1\n"))
	})))
	request := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Body.String() != "mxhook_test 1\n" {
		t.Fatalf("metrics response = %d %q", response.Code, response.Body.String())
	}
}
