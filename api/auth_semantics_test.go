package api_test

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mxhook/mxhook/api"
)

type stubAuthorizer struct {
	err error
}

func (a *stubAuthorizer) Authorize(r *http.Request, _ api.Permission) (*http.Request, error) {
	if a.err != nil {
		return nil, a.err
	}
	return r, nil
}

// An authenticated caller lacking a permission is a different failure from a
// caller who never presented a credential: 403 vs 401.
func TestAuthorizerErrorsSplitUnauthenticatedFromForbidden(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{name: "missing credential", err: api.ErrUnauthenticated, want: http.StatusUnauthorized},
		{name: "wrapped missing credential", err: fmt.Errorf("%w: no key presented", api.ErrUnauthenticated), want: http.StatusUnauthorized},
		{name: "insufficient permission", err: api.ErrForbidden, want: http.StatusForbidden},
		{name: "wrapped insufficient permission", err: fmt.Errorf("%w: needs routes:write", api.ErrForbidden), want: http.StatusForbidden},
		{name: "unclassified error stays forbidden", err: errors.New("boom"), want: http.StatusForbidden},
		{name: "authorized", err: nil, want: http.StatusOK},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			srv := api.NewServer(api.Config{APIKey: "secret"},
				api.WithAuthorizer(&stubAuthorizer{err: test.err}))
			req := httptest.NewRequest(http.MethodGet, "/messages", nil)
			w := httptest.NewRecorder()
			srv.Handler().ServeHTTP(w, req)
			if w.Code != test.want {
				t.Fatalf("status = %d, want %d (body %s)", w.Code, test.want, w.Body.String())
			}
		})
	}
}

// A server with neither a static key nor an authorizer must not serve the
// control plane; it fails closed rather than exposing every route.
func TestControlRoutesFailClosedWithoutCredentialSource(t *testing.T) {
	srv := api.NewServer(api.Config{})
	for _, path := range []string{"/messages", "/domains", "/routes", "/dlq"} {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			w := httptest.NewRecorder()
			srv.Handler().ServeHTTP(w, req)
			if w.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401 (body %s)", w.Code, w.Body.String())
			}
		})
	}
}

// Unauthenticated liveness and readiness probes must keep working, since
// orchestrators call them without credentials.
func TestHealthEndpointsRemainUnauthenticated(t *testing.T) {
	srv := api.NewServer(api.Config{})
	for _, path := range []string{"/healthz", "/readyz"} {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			w := httptest.NewRecorder()
			srv.Handler().ServeHTTP(w, req)
			if w.Code == http.StatusUnauthorized {
				t.Fatalf("%s requires authentication", path)
			}
		})
	}
}
