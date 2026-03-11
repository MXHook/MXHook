package router_test

import (
	"testing"

	"github.com/mxhook/mxhook/internal/types"
	"github.com/mxhook/mxhook/router"
)

func TestAdvancedMatcher_RegexRecipient(t *testing.T) {
	rule := router.AdvancedRule{
		RecipientRegex: `^support\+.+@example\.com$`,
		Target:         router.TargetWebhook,
		WebhookURL:     "https://x.com/hook",
	}
	m, err := router.NewAdvancedMatcher([]router.AdvancedRule{rule})
	if err != nil {
		t.Fatalf("NewAdvancedMatcher: %v", err)
	}

	event := &types.EmailEvent{
		EnvelopeTo: []string{"support+ticket@example.com"},
	}
	matched, ok := m.Match(event)
	if !ok {
		t.Fatal("expected regex match")
	}
	if matched.WebhookURL != "https://x.com/hook" {
		t.Errorf("wrong URL: %q", matched.WebhookURL)
	}
}

func TestAdvancedMatcher_HeaderMatch(t *testing.T) {
	rule := router.AdvancedRule{
		HeaderConditions: map[string]string{
			"X-Priority": "urgent",
		},
		Target:     router.TargetWebhook,
		WebhookURL: "https://x.com/urgent",
	}
	m, err := router.NewAdvancedMatcher([]router.AdvancedRule{rule})
	if err != nil {
		t.Fatalf("NewAdvancedMatcher: %v", err)
	}

	event := &types.EmailEvent{
		Headers: map[string][]string{
			"X-Priority": {"urgent"},
		},
	}
	matched, ok := m.Match(event)
	if !ok {
		t.Fatal("expected header match")
	}
	if matched.WebhookURL != "https://x.com/urgent" {
		t.Errorf("wrong URL: %q", matched.WebhookURL)
	}
}

func TestAdvancedMatcher_NoMatch(t *testing.T) {
	rule := router.AdvancedRule{
		RecipientRegex: `^billing@example\.com$`,
		Target:         router.TargetWebhook,
	}
	m, _ := router.NewAdvancedMatcher([]router.AdvancedRule{rule})
	event := &types.EmailEvent{EnvelopeTo: []string{"other@example.com"}}
	_, ok := m.Match(event)
	if ok {
		t.Fatal("expected no match")
	}
}
