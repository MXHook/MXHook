package smtp_test

import (
	"testing"

	mxsmtp "github.com/mxhook/mxhook/smtp"
)

func TestNewServer_ReturnsNonNil(t *testing.T) {
	cfg := mxsmtp.Config{
		Addr:            ":2525",
		Domain:          "example.com",
		AllowedDomains:  []string{"example.com"},
		MaxMessageBytes: 10 * 1024 * 1024,
		MaxRecipients:   10,
	}
	s := mxsmtp.NewServer(cfg, nil)
	if s == nil {
		t.Fatal("expected non-nil server")
	}
}
