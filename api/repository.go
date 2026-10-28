package api

import (
	"context"
	"errors"
)

var ErrNotFound = errors.New("api: not found")

// ErrInvalid marks client input the repository rejected; handlers map it to
// HTTP 400.
var ErrInvalid = errors.New("api: invalid input")

// ErrConflict marks a uniqueness conflict with existing state; handlers map
// it to HTTP 409.
var ErrConflict = errors.New("api: conflict")

// ErrUnauthenticated marks a caller that presented no usable credential; the
// auth middleware maps it to HTTP 401. An Authorizer returns it to
// distinguish a missing credential from an insufficient one.
var ErrUnauthenticated = errors.New("api: unauthenticated")

// ErrForbidden marks an authenticated caller lacking the required
// permission; the auth middleware maps it to HTTP 403. Authorizer errors of
// no recognized kind are treated as forbidden.
var ErrForbidden = errors.New("api: forbidden")

type Page struct {
	Limit  int
	Offset int
}

type Repository interface {
	CreateDomain(ctx context.Context, name string) (Domain, error)
	ListDomains(ctx context.Context) ([]Domain, error)
	DeleteDomain(ctx context.Context, id string) error
	CreateRoute(ctx context.Context, route Route) (Route, error)
	ListRoutes(ctx context.Context) ([]Route, error)
	DeleteRoute(ctx context.Context, id string) error
	ListMessages(ctx context.Context, page Page) ([]Message, error)
	GetMessage(ctx context.Context, id string) (Message, error)
	ListDeadDeliveries(ctx context.Context, page Page) ([]DLQEntry, error)
	DeleteIngressFailure(ctx context.Context, messageID string) error
	ScheduleReplay(ctx context.Context, messageID string) (int64, error)
}

type ReadinessChecker interface {
	Ping(ctx context.Context) error
}
