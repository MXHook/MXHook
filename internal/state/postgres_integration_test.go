package state

import (
	"bytes"
	"context"
	"os"
	"testing"
	"time"

	"github.com/mxhook/mxhook/router"
	"github.com/mxhook/mxhook/types"
)

func TestPostgresOutboxAndControlPlaneIntegration(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	store, err := Open(Config{
		Driver: "postgres", DSN: dsn, EncryptionKey: bytes.Repeat([]byte{8}, 32),
		LeaseDuration: time.Second, PollInterval: 5 * time.Millisecond, RunMigrations: true,
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = store.Close() }()
	suffix := time.Now().UTC().Format("20060102T150405.000000000")
	rule := router.Rule{
		ID: "pg-route-" + suffix, Domain: "example.com", RecipientPattern: "*@example.com",
		Target: router.TargetWebhook, WebhookURL: "https://example.com/hook", WebhookSecret: "postgres-secret",
	}
	if err := store.EnsureRoute(context.Background(), rule); err != nil {
		t.Fatalf("EnsureRoute: %v", err)
	}
	event := &types.EmailEvent{
		SchemaVersion: types.EmailEventSchemaV1, ID: "pg-message-" + suffix, TraceID: "pg-trace",
		ReceivedAt: time.Now().UTC(), EnvelopeFrom: "sender@example.net", EnvelopeTo: []string{"inbox@example.com"},
		Headers: map[string][]string{}, Subject: "postgres", RawPath: "/raw/postgres.eml",
	}
	if err := store.Commit(context.Background(), event, []router.Rule{rule}); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	deliveryID := deterministicDeliveryID(event.ID, rule.ID)
	record, err := store.GetDelivery(context.Background(), deliveryID)
	if err != nil || record.State != "pending" {
		t.Fatalf("GetDelivery = %#v, %v", record, err)
	}
	delivery, err := store.ClaimDelivery(context.Background())
	if err != nil {
		t.Fatalf("ClaimDelivery: %v", err)
	}
	if delivery.ID != deliveryID || delivery.Secret != rule.WebhookSecret {
		t.Fatalf("claimed delivery = %#v", delivery)
	}
	if err := store.CompleteDelivery(context.Background(), delivery); err != nil {
		t.Fatalf("CompleteDelivery: %v", err)
	}
	domain, err := store.CreateDomain(context.Background(), "pg-"+suffix+".example")
	if err != nil || domain.ID == "" {
		t.Fatalf("CreateDomain = %#v, %v", domain, err)
	}
}
