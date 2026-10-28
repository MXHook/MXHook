package state

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/mxhook/mxhook/api"
)

func TestCreateDomainReturnsTypedErrors(t *testing.T) {
	store := openTestStore(t, filepath.Join(t.TempDir(), "mxhook.db"), bytes.Repeat([]byte{4}, 32), time.Second)
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()

	if _, err := store.CreateDomain(ctx, ""); !errors.Is(err, api.ErrInvalid) {
		t.Fatalf("empty name error = %v, want api.ErrInvalid", err)
	}
	if _, err := store.CreateDomain(ctx, "user@example.com"); !errors.Is(err, api.ErrInvalid) {
		t.Fatalf("address-like name error = %v, want api.ErrInvalid", err)
	}
	if _, err := store.CreateDomain(ctx, "example.com"); err != nil {
		t.Fatalf("CreateDomain: %v", err)
	}
	if _, err := store.CreateDomain(ctx, "example.com"); !errors.Is(err, api.ErrConflict) {
		t.Fatalf("duplicate domain error = %v, want api.ErrConflict", err)
	}
}

func TestCreateRouteReturnsInvalidForBadInput(t *testing.T) {
	store := openTestStore(t, filepath.Join(t.TempDir(), "mxhook.db"), bytes.Repeat([]byte{5}, 32), time.Second)
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()

	missingSecret := api.Route{Domain: "example.com", RecipientPattern: "*@example.com", WebhookURL: "https://example.com/hook"}
	if _, err := store.CreateRoute(ctx, missingSecret); !errors.Is(err, api.ErrInvalid) {
		t.Fatalf("missing secret error = %v, want api.ErrInvalid", err)
	}
	missingPattern := api.Route{Domain: "example.com", WebhookURL: "https://example.com/hook", WebhookSecret: "secret"}
	if _, err := store.CreateRoute(ctx, missingPattern); !errors.Is(err, api.ErrInvalid) {
		t.Fatalf("missing pattern error = %v, want api.ErrInvalid", err)
	}
	missingURL := api.Route{Domain: "example.com", RecipientPattern: "*@example.com", WebhookSecret: "secret"}
	if _, err := store.CreateRoute(ctx, missingURL); !errors.Is(err, api.ErrInvalid) {
		t.Fatalf("missing URL error = %v, want api.ErrInvalid", err)
	}
}
