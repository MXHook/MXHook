package smtp_test

import (
	"context"
	"testing"

	"github.com/mxhook/mxhook/queue"
	mxsmtp "github.com/mxhook/mxhook/smtp"
)

type testEnqueuer struct{}

func (testEnqueuer) Enqueue(context.Context, queue.Message) error { return nil }

func TestNewServer_ReturnsNonNil(t *testing.T) {
	cfg := mxsmtp.Config{
		Addr:            ":2525",
		Domain:          "example.com",
		AllowedDomains:  []string{"example.com"},
		MaxMessageBytes: 10 * 1024 * 1024,
		MaxRecipients:   10,
	}
	s, err := mxsmtp.NewServer(cfg, testEnqueuer{})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	if s == nil {
		t.Fatal("expected non-nil server")
	}
}

func TestNewServer_RejectsCommaInAllowedDomain(t *testing.T) {
	cfg := mxsmtp.Config{
		Addr:            ":2525",
		Domain:          "example.com",
		AllowedDomains:  []string{"example.com,example.net"},
		MaxMessageBytes: 10 * 1024 * 1024,
		MaxRecipients:   10,
	}
	if _, err := mxsmtp.NewServer(cfg, testEnqueuer{}); err == nil {
		t.Fatal("NewServer accepted comma-containing allowed domain")
	}
}
