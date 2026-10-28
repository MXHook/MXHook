package api_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/mxhook/mxhook/api"
)

// countingInvalidator records how often the ingress cache was told to drop its
// copy of the domain set.
type countingInvalidator struct{ calls int32 }

func (c *countingInvalidator) InvalidateDomains() { atomic.AddInt32(&c.calls, 1) }

func (c *countingInvalidator) Calls() int { return int(atomic.LoadInt32(&c.calls)) }

func TestCreateDomainInvalidatesIngressCache(t *testing.T) {
	invalidator := &countingInvalidator{}
	srv := newTestServer(api.WithDomainInvalidator(invalidator))

	req := authed(httptest.NewRequest(http.MethodPost, "/domains", strings.NewReader(`{"name":"example.com"}`)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /domains = %d, want 201", rec.Code)
	}
	if invalidator.Calls() != 1 {
		t.Fatalf("invalidations after create = %d, want 1", invalidator.Calls())
	}
}

func TestDeleteDomainInvalidatesIngressCache(t *testing.T) {
	invalidator := &countingInvalidator{}
	srv := newTestServer(api.WithDomainInvalidator(invalidator))

	create := authed(httptest.NewRequest(http.MethodPost, "/domains", strings.NewReader(`{"name":"example.com"}`)))
	create.Header.Set("Content-Type", "application/json")
	createRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(createRec, create)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("POST /domains = %d, want 201", createRec.Code)
	}

	created := decodeJSON[api.Domain](t, createRec)

	del := authed(httptest.NewRequest(http.MethodDelete, "/domains/"+created.ID, nil))
	delRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(delRec, del)

	if delRec.Code != http.StatusNoContent {
		t.Fatalf("DELETE /domains/{id} = %d, want 204", delRec.Code)
	}
	if invalidator.Calls() != 2 {
		t.Fatalf("invalidations after create+delete = %d, want 2", invalidator.Calls())
	}
}

// A rejected write must not invalidate: dropping the cache on a no-op write
// would let malformed requests pressure the ingress path.
func TestRejectedDomainWriteDoesNotInvalidate(t *testing.T) {
	invalidator := &countingInvalidator{}
	srv := newTestServer(api.WithDomainInvalidator(invalidator))

	req := authed(httptest.NewRequest(http.MethodPost, "/domains", strings.NewReader(`{"name":""}`)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("POST /domains with empty name = %d, want 400", rec.Code)
	}
	if invalidator.Calls() != 0 {
		t.Fatalf("invalidations after rejected write = %d, want 0", invalidator.Calls())
	}
}

// The invalidator is optional; a server without one must still serve writes.
func TestDomainWriteWithoutInvalidatorSucceeds(t *testing.T) {
	srv := newTestServer()

	req := authed(httptest.NewRequest(http.MethodPost, "/domains", strings.NewReader(`{"name":"example.com"}`)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /domains without invalidator = %d, want 201", rec.Code)
	}
}
