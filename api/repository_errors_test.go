package api_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mxhook/mxhook/api"
)

type erroringRepository struct {
	err error
}

func (r *erroringRepository) CreateDomain(context.Context, string) (api.Domain, error) {
	return api.Domain{}, r.err
}

func (r *erroringRepository) ListDomains(context.Context) ([]api.Domain, error) { return nil, r.err }
func (r *erroringRepository) DeleteDomain(context.Context, string) error        { return r.err }
func (r *erroringRepository) CreateRoute(context.Context, api.Route) (api.Route, error) {
	return api.Route{}, r.err
}
func (r *erroringRepository) ListRoutes(context.Context) ([]api.Route, error) { return nil, r.err }
func (r *erroringRepository) DeleteRoute(context.Context, string) error       { return r.err }
func (r *erroringRepository) ListMessages(context.Context, api.Page) ([]api.Message, error) {
	return nil, r.err
}

func (r *erroringRepository) GetMessage(context.Context, string) (api.Message, error) {
	return api.Message{}, r.err
}

func (r *erroringRepository) ListDeadDeliveries(context.Context, api.Page) ([]api.DLQEntry, error) {
	return nil, r.err
}
func (r *erroringRepository) DeleteIngressFailure(context.Context, string) error { return r.err }

func (r *erroringRepository) ScheduleReplay(context.Context, string) (int64, error) {
	return 0, r.err
}

func TestRepositoryErrorsMapToClientStatusCodes(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{name: "invalid input", err: fmt.Errorf("%w: valid domain name is required", api.ErrInvalid), want: http.StatusBadRequest},
		{name: "conflict", err: fmt.Errorf("%w: domain exists", api.ErrConflict), want: http.StatusConflict},
		{name: "not found", err: api.ErrNotFound, want: http.StatusNotFound},
		{name: "internal", err: errors.New("disk on fire"), want: http.StatusInternalServerError},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			srv := newTestServer(api.WithRepository(&erroringRepository{err: test.err}))
			req := authed(httptest.NewRequest(http.MethodPost, "/domains",
				bytes.NewBufferString(`{"name":"example.com"}`)))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			srv.Handler().ServeHTTP(w, req)
			if w.Code != test.want {
				t.Fatalf("status = %d, want %d (body %s)", w.Code, test.want, w.Body.String())
			}
			if test.want == http.StatusInternalServerError && bytes.Contains(w.Body.Bytes(), []byte("disk on fire")) {
				t.Fatal("internal error detail leaked to client")
			}
		})
	}
}
