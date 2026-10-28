package router_test

import (
	"testing"

	"github.com/mxhook/mxhook/router"
)

func TestDomainConfig_MatchesDomain(t *testing.T) {
	cfg := router.DomainConfig{
		Domain: "example.com",
		Rules: []router.Rule{
			{RecipientPattern: "support@example.com", Target: router.TargetWebhook, WebhookURL: "https://x.com/hook"},
		},
		CatchAll: &router.Rule{Target: router.TargetWebhook, WebhookURL: "https://x.com/catchall"},
	}

	m := router.NewDomainMatcher([]router.DomainConfig{cfg})

	// specific rule match
	rule, ok := m.MatchRecipient("support@example.com")
	if !ok {
		t.Fatal("expected match for support@example.com")
	}
	if rule.WebhookURL != "https://x.com/hook" {
		t.Errorf("wrong webhook URL: %q", rule.WebhookURL)
	}

	// catch-all fallback
	rule, ok = m.MatchRecipient("unknown@example.com")
	if !ok {
		t.Fatal("expected catch-all match")
	}
	if rule.WebhookURL != "https://x.com/catchall" {
		t.Errorf("wrong catch-all URL: %q", rule.WebhookURL)
	}

	// no match for unknown domain
	_, ok = m.MatchRecipient("user@other.com")
	if ok {
		t.Fatal("expected no match for unknown domain")
	}
}
