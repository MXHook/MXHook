package postgres

import (
	"context"
	"strings"
	"testing"
)

func TestMigrationsDisabledRequiresExistingSchemaIntegration(t *testing.T) {
	dsn := newIntegrationDSN(t)
	ctx := context.Background()
	adapter, err := NewWithConfig(ctx, Config{DSN: dsn, RunMigrations: false})
	if err == nil {
		adapter.Close()
		t.Fatal("NewWithConfig accepted a database without a queue schema")
	}
	if !strings.Contains(err.Error(), "schema") {
		t.Fatalf("error = %v, want schema verification failure", err)
	}
}

func TestQueueStateCheckConstraintIntegration(t *testing.T) {
	dsn := newIntegrationDSN(t)
	ctx := context.Background()
	adapter, err := New(ctx, dsn)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer adapter.Close()

	// The sqlite schema has always constrained state; postgres must match.
	if _, err := adapter.pool.Exec(ctx,
		`INSERT INTO mxhook_queue (id, payload, state) VALUES ($1, $2::jsonb, $3)`,
		"bad-state", []byte(`{}`), "bogus",
	); err == nil {
		t.Fatal("postgres accepted an unknown queue state")
	}
	for _, state := range []string{"pending", "processing", "acked", "nacked"} {
		if _, err := adapter.pool.Exec(ctx,
			`INSERT INTO mxhook_queue (id, payload, state) VALUES ($1, $2::jsonb, $3)`,
			"state-"+state, []byte(`{}`), state,
		); err != nil {
			t.Errorf("postgres rejected valid state %q: %v", state, err)
		}
	}
}

func TestSchemaIsIdempotentAcrossRestartsIntegration(t *testing.T) {
	dsn := newIntegrationDSN(t)
	ctx := context.Background()
	// Re-running the DDL must not fail on the separately-added constraint.
	for range 3 {
		adapter, err := New(ctx, dsn)
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		adapter.Close()
	}
}

func TestMigrationsDisabledUsesExistingSchemaIntegration(t *testing.T) {
	dsn := newIntegrationDSN(t)
	ctx := context.Background()
	initial, err := New(ctx, dsn)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	initial.Close()
	adapter, err := NewWithConfig(ctx, Config{DSN: dsn, RunMigrations: false})
	if err != nil {
		t.Fatalf("NewWithConfig without migrations: %v", err)
	}
	defer adapter.Close()
	if err := adapter.Enqueue(ctx, integrationMessage("gated", []byte("body"))); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
}
