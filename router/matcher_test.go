package router_test

import (
	"testing"

	"github.com/mxhook/mxhook/router"
)

func TestMatcher_ExactAddress(t *testing.T) {
	rule := router.Rule{
		RecipientPattern: "support@example.com",
		Target:           router.TargetWebhook,
		WebhookURL:       "https://example.com/hook",
	}
	m := router.NewMatcher([]router.Rule{rule})

	matched, ok := m.Match("support@example.com")
	if !ok {
		t.Fatal("expected match")
	}
	if matched.WebhookURL != "https://example.com/hook" {
		t.Errorf("wrong webhook URL: %q", matched.WebhookURL)
	}
}

func TestMatcher_WildcardDomain(t *testing.T) {
	rule := router.Rule{RecipientPattern: "*@example.com", Target: router.TargetWebhook}
	m := router.NewMatcher([]router.Rule{rule})
	_, ok := m.Match("anything@example.com")
	if !ok {
		t.Fatal("expected wildcard match")
	}
}

func TestMatcher_PlusAddressing(t *testing.T) {
	rule := router.Rule{RecipientPattern: "task@example.com", Target: router.TargetWebhook}
	m := router.NewMatcher([]router.Rule{rule})
	_, ok := m.Match("task+label@example.com")
	if !ok {
		t.Fatal("expected plus-address match")
	}
}

func TestMatcher_NoMatch(t *testing.T) {
	rule := router.Rule{RecipientPattern: "support@example.com", Target: router.TargetWebhook}
	m := router.NewMatcher([]router.Rule{rule})
	_, ok := m.Match("other@example.com")
	if ok {
		t.Fatal("expected no match")
	}
}
