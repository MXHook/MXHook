package router_test

import (
	"testing"

	"github.com/mxhook/mxhook/router"
)

func TestEngine_ImplementsInterface(t *testing.T) {
	var _ router.RuleLoader = (*router.Engine)(nil)
}

func TestEngine_InMemory(t *testing.T) {
	rules := []router.Rule{
		{RecipientPattern: "support@example.com", Target: router.TargetWebhook, WebhookURL: "https://x.com/hook"},
	}
	engine := router.NewEngineInMemory(rules)
	loaded, err := engine.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(loaded))
	}
}
