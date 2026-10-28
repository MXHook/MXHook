package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mxhook/mxhook/api"
)

func decodeJSON[T any](t *testing.T, w *httptest.ResponseRecorder) T {
	t.Helper()
	var value T
	if err := json.NewDecoder(w.Body).Decode(&value); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return value
}

func TestDeleteRouteRemovesItFromListing(t *testing.T) {
	srv := newTestServer()

	create := authed(httptest.NewRequest(http.MethodPost, "/routes",
		bytes.NewBufferString(`{"domain":"example.com","recipient_pattern":"*@example.com","webhook_url":"https://example.com/hook"}`)))
	create.Header.Set("Content-Type", "application/json")
	created := httptest.NewRecorder()
	srv.Handler().ServeHTTP(created, create)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want 201", created.Code)
	}
	route := decodeJSON[api.Route](t, created)

	deleted := httptest.NewRecorder()
	srv.Handler().ServeHTTP(deleted, authed(httptest.NewRequest(http.MethodDelete, "/routes/"+route.ID, nil)))
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, want 204", deleted.Code)
	}

	listed := httptest.NewRecorder()
	srv.Handler().ServeHTTP(listed, authed(httptest.NewRequest(http.MethodGet, "/routes", nil)))
	for _, remaining := range decodeJSON[[]api.Route](t, listed) {
		if remaining.ID == route.ID {
			t.Fatalf("route %s still listed after delete", route.ID)
		}
	}
}

func TestDeleteRouteUnknownIDReturnsNotFound(t *testing.T) {
	srv := newTestServer()
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, authed(httptest.NewRequest(http.MethodDelete, "/routes/does-not-exist", nil)))
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}

func TestDeleteDomainRemovesItFromListing(t *testing.T) {
	srv := newTestServer()

	create := authed(httptest.NewRequest(http.MethodPost, "/domains",
		bytes.NewBufferString(`{"name":"example.com"}`)))
	create.Header.Set("Content-Type", "application/json")
	created := httptest.NewRecorder()
	srv.Handler().ServeHTTP(created, create)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want 201", created.Code)
	}
	domain := decodeJSON[api.Domain](t, created)

	deleted := httptest.NewRecorder()
	srv.Handler().ServeHTTP(deleted, authed(httptest.NewRequest(http.MethodDelete, "/domains/"+domain.ID, nil)))
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, want 204", deleted.Code)
	}

	listed := httptest.NewRecorder()
	srv.Handler().ServeHTTP(listed, authed(httptest.NewRequest(http.MethodGet, "/domains", nil)))
	for _, remaining := range decodeJSON[[]api.Domain](t, listed) {
		if remaining.ID == domain.ID {
			t.Fatalf("domain %s still listed after delete", domain.ID)
		}
	}
}

func TestDeleteDomainUnknownIDReturnsNotFound(t *testing.T) {
	srv := newTestServer()
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, authed(httptest.NewRequest(http.MethodDelete, "/domains/does-not-exist", nil)))
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}

// Routes are registered per method, so an unregistered method on a known path
// must be refused rather than falling through to another handler.
func TestUnsupportedMethodsAreRejected(t *testing.T) {
	srv := newTestServer()
	for _, test := range []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/dlq"},
		{http.MethodDelete, "/dlq"},
		{http.MethodPut, "/domains"},
		{http.MethodPut, "/routes"},
		{http.MethodPost, "/messages"},
	} {
		t.Run(test.method+" "+test.path, func(t *testing.T) {
			w := httptest.NewRecorder()
			srv.Handler().ServeHTTP(w, authed(httptest.NewRequest(test.method, test.path, nil)))
			if w.Code != http.StatusMethodNotAllowed && w.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want 405 or 404", w.Code)
			}
		})
	}
}

func TestInvalidCreateBodiesReturnBadRequest(t *testing.T) {
	for _, test := range []struct {
		name string
		path string
		body string
	}{
		{name: "domain malformed json", path: "/domains", body: `{`},
		{name: "domain empty name", path: "/domains", body: `{"name":""}`},
		{name: "domain missing name", path: "/domains", body: `{}`},
		{name: "route malformed json", path: "/routes", body: `{`},
	} {
		t.Run(test.name, func(t *testing.T) {
			srv := newTestServer()
			req := authed(httptest.NewRequest(http.MethodPost, test.path, bytes.NewBufferString(test.body)))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			srv.Handler().ServeHTTP(w, req)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (body %s)", w.Code, w.Body.String())
			}
		})
	}
}
