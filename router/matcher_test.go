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

func TestMatcher_MixedCaseRecipientMatchesWildcardDomain(t *testing.T) {
	rule := router.Rule{RecipientPattern: "*@example.com", Target: router.TargetWebhook}
	m := router.NewMatcher([]router.Rule{rule})
	if _, ok := m.Match("User@EXAMPLE.COM"); !ok {
		t.Fatal("expected mixed-case recipient to match wildcard domain pattern")
	}
}

func TestMatcher_MixedCaseRecipientMatchesExactAddress(t *testing.T) {
	rule := router.Rule{RecipientPattern: "alice@example.com", Target: router.TargetWebhook}
	m := router.NewMatcher([]router.Rule{rule})
	if _, ok := m.Match("Alice@Example.Com"); !ok {
		t.Fatal("expected mixed-case recipient to match exact-address pattern")
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

func TestMatcher_GlobalCatchAll(t *testing.T) {
	m := router.NewMatcher([]router.Rule{{ID: "catch-all", RecipientPattern: "*"}})
	if rule, ok := m.Match("anything@unknown.example"); !ok || rule.ID != "catch-all" {
		t.Fatalf("global catch-all result = (%#v, %v)", rule, ok)
	}
}
