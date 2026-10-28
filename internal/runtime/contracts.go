package runtime

import (
	"context"
	"errors"
	"io"

	"github.com/mxhook/mxhook/router"
	"github.com/mxhook/mxhook/types"
	"github.com/mxhook/mxhook/webhook"
)

type Parser interface {
	Parse(id, envelopeFrom string, envelopeTo []string, raw io.Reader) (*types.EmailEvent, error)
}

type RawStore interface {
	Store(ctx context.Context, id string, body []byte) (string, error)
}

type RuleLoader interface {
	Load() ([]router.Rule, error)
}

type Deliverer interface {
	Deliver(ctx context.Context, endpoint webhook.Endpoint, event *types.EmailEvent) error
}

type permanentError struct{ error }

// Unwrap exposes the wrapped cause so errors.Is/As can match through
// Permanent.
func (e permanentError) Unwrap() error { return e.error }

func Permanent(err error) error {
	if err == nil {
		return nil
	}
	return permanentError{error: err}
}

func IsPermanent(err error) bool {
	var target permanentError
	return errors.As(err, &target)
}
