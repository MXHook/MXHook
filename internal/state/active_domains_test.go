package state_test

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/mxhook/mxhook/internal/state"
)

func openDomainStore(t *testing.T) *state.Store {
	t.Helper()
	store, err := state.Open(state.Config{
		Driver: "sqlite", DSN: filepath.Join(t.TempDir(), "mxhook.db"), EncryptionKey: bytes.Repeat([]byte{9}, 32),
		LeaseDuration: time.Second, PollInterval: time.Millisecond, RunMigrations: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

// ActiveDomains is what SMTP recipient acceptance reads, so a domain created
// through the control plane must appear in it.
func TestActiveDomainsReturnsCreatedDomains(t *testing.T) {
	store := openDomainStore(t)
	ctx := context.Background()

	domains, err := store.ActiveDomains(ctx)
	if err != nil {
		t.Fatalf("ActiveDomains on empty store: %v", err)
	}
	if len(domains) != 0 {
		t.Fatalf("ActiveDomains on empty store = %v, want empty", domains)
	}

	if _, err := store.CreateDomain(ctx, "Example.COM"); err != nil {
		t.Fatalf("CreateDomain: %v", err)
	}

	domains, err = store.ActiveDomains(ctx)
	if err != nil {
		t.Fatalf("ActiveDomains: %v", err)
	}
	if len(domains) != 1 || domains[0] != "example.com" {
		t.Fatalf("ActiveDomains = %v, want [example.com] (normalised)", domains)
	}
}

func TestActiveDomainsDropsDeletedDomains(t *testing.T) {
	store := openDomainStore(t)
	ctx := context.Background()

	domain, err := store.CreateDomain(ctx, "temporary.example")
	if err != nil {
		t.Fatalf("CreateDomain: %v", err)
	}
	if err := store.DeleteDomain(ctx, domain.ID); err != nil {
		t.Fatalf("DeleteDomain: %v", err)
	}

	domains, err := store.ActiveDomains(ctx)
	if err != nil {
		t.Fatalf("ActiveDomains: %v", err)
	}
	if len(domains) != 0 {
		t.Fatalf("ActiveDomains after delete = %v, want empty", domains)
	}
}
