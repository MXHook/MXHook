package router_test

import (
	"testing"

	"github.com/mxhook/mxhook/internal/types"
	"github.com/mxhook/mxhook/router"
)

// ---- Rule constants ----

func TestTargetConstants(t *testing.T) {
	if router.TargetWebhook != "webhook" {
		t.Errorf("TargetWebhook = %q, want %q", router.TargetWebhook, "webhook")
	}
	if router.TargetQueue != "queue" {
		t.Errorf("TargetQueue = %q, want %q", router.TargetQueue, "queue")
	}
	if router.TargetStorage != "storage" {
		t.Errorf("TargetStorage = %q, want %q", router.TargetStorage, "storage")
	}
}

func TestRule_ZeroValue(t *testing.T) {
	var r router.Rule
	if r.IsCatchAll {
		t.Error("zero-value Rule.IsCatchAll should be false")
	}
	if r.Priority != 0 {
		t.Errorf("zero-value Rule.Priority = %d, want 0", r.Priority)
	}
	if r.Target != "" {
		t.Errorf("zero-value Rule.Target = %q, want empty", r.Target)
	}
}

func TestRule_FullFields(t *testing.T) {
	r := router.Rule{
		ID:               "rule-1",
		Domain:           "example.com",
		RecipientPattern: "*@example.com",
		Target:           router.TargetWebhook,
		WebhookURL:       "https://hooks.example.com/inbox",
		WebhookSecret:    "s3cr3t",
		IsCatchAll:       true,
		Priority:         10,
	}
	if r.ID != "rule-1" {
		t.Errorf("ID = %q", r.ID)
	}
	if r.Domain != "example.com" {
		t.Errorf("Domain = %q", r.Domain)
	}
	if r.WebhookSecret != "s3cr3t" {
		t.Errorf("WebhookSecret = %q", r.WebhookSecret)
	}
	if !r.IsCatchAll {
		t.Error("expected IsCatchAll = true")
	}
	if r.Priority != 10 {
		t.Errorf("Priority = %d, want 10", r.Priority)
	}
}

// ---- Engine additional coverage ----

func TestEngine_InMemory_Empty(t *testing.T) {
	e := router.NewEngineInMemory(nil)
	rules, err := e.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(rules) != 0 {
		t.Errorf("expected 0 rules, got %d", len(rules))
	}
}

func TestEngine_InMemory_Multiple(t *testing.T) {
	rules := []router.Rule{
		{ID: "1", RecipientPattern: "a@example.com", Target: router.TargetWebhook},
		{ID: "2", RecipientPattern: "b@example.com", Target: router.TargetQueue},
		{ID: "3", RecipientPattern: "*@example.com", Target: router.TargetStorage, IsCatchAll: true},
	}
	e := router.NewEngineInMemory(rules)
	loaded, err := e.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(loaded) != 3 {
		t.Fatalf("expected 3 rules, got %d", len(loaded))
	}
	// Verify it returns a copy (mutation must not affect engine)
	loaded[0].ID = "mutated"
	reloaded, _ := e.Load()
	if reloaded[0].ID == "mutated" {
		t.Error("Load returned a reference, not a copy")
	}
}

func TestEngine_Close_NilTicker(t *testing.T) {
	// NewEngineInMemory does not set ticker; Close must not panic
	e := router.NewEngineInMemory([]router.Rule{})
	e.Close() // must not panic
}

// ---- Matcher additional coverage ----

func TestMatcher_EmptyRules(t *testing.T) {
	m := router.NewMatcher(nil)
	_, ok := m.Match("anyone@example.com")
	if ok {
		t.Error("expected no match with empty rules")
	}
}

func TestMatcher_NoAtSign(t *testing.T) {
	// normalizeRecipient: address without "@" is returned as-is
	rule := router.Rule{RecipientPattern: "localonly", Target: router.TargetWebhook}
	m := router.NewMatcher([]router.Rule{rule})
	_, ok := m.Match("localonly")
	if !ok {
		t.Error("expected match for address without @")
	}
}

func TestMatcher_WildcardDoesNotMatchSubdomain(t *testing.T) {
	rule := router.Rule{RecipientPattern: "*@example.com", Target: router.TargetWebhook}
	m := router.NewMatcher([]router.Rule{rule})
	// "user@sub.example.com" should NOT match "*@example.com"
	_, ok := m.Match("user@sub.example.com")
	if ok {
		t.Error("wildcard *@example.com must not match sub.example.com")
	}
}

func TestMatcher_PlusNormalizationNoPlus(t *testing.T) {
	rule := router.Rule{RecipientPattern: "info@example.com", Target: router.TargetWebhook}
	m := router.NewMatcher([]router.Rule{rule})
	_, ok := m.Match("info@example.com")
	if !ok {
		t.Error("expected plain address to match")
	}
}

func TestMatcher_MultipleRules_FirstWins(t *testing.T) {
	rules := []router.Rule{
		{RecipientPattern: "support@example.com", Target: router.TargetWebhook, WebhookURL: "https://first.com"},
		{RecipientPattern: "support@example.com", Target: router.TargetWebhook, WebhookURL: "https://second.com"},
	}
	m := router.NewMatcher(rules)
	rule, ok := m.Match("support@example.com")
	if !ok {
		t.Fatal("expected match")
	}
	if rule.WebhookURL != "https://first.com" {
		t.Errorf("expected first rule to win, got %q", rule.WebhookURL)
	}
}

// ---- DomainMatcher additional coverage ----

func TestDomainMatcher_NoCatchAll(t *testing.T) {
	cfg := router.DomainConfig{
		Domain: "example.com",
		Rules: []router.Rule{
			{RecipientPattern: "support@example.com", Target: router.TargetWebhook},
		},
		CatchAll: nil,
	}
	m := router.NewDomainMatcher([]router.DomainConfig{cfg})
	_, ok := m.MatchRecipient("unknown@example.com")
	if ok {
		t.Error("expected no match when no catch-all and no specific rule")
	}
}

func TestDomainMatcher_CaseInsensitiveDomain(t *testing.T) {
	cfg := router.DomainConfig{
		Domain:   "Example.COM",
		Rules:    []router.Rule{{RecipientPattern: "hi@example.com", Target: router.TargetWebhook, WebhookURL: "https://x.com"}},
		CatchAll: nil,
	}
	m := router.NewDomainMatcher([]router.DomainConfig{cfg})
	// domain lookup is lowercased
	rule, ok := m.MatchRecipient("hi@example.com")
	if !ok {
		t.Fatal("expected match despite mixed-case domain in config")
	}
	if rule.WebhookURL != "https://x.com" {
		t.Errorf("wrong URL: %q", rule.WebhookURL)
	}
}

func TestDomainMatcher_NoAtSignRecipient(t *testing.T) {
	cfg := router.DomainConfig{
		Domain:   "example.com",
		CatchAll: &router.Rule{Target: router.TargetWebhook},
	}
	m := router.NewDomainMatcher([]router.DomainConfig{cfg})
	// recipientDomain returns "" for no "@"; no domain match expected
	_, ok := m.MatchRecipient("notanemail")
	if ok {
		t.Error("expected no match for non-email address")
	}
}

func TestDomainMatcher_EmptyConfigs(t *testing.T) {
	m := router.NewDomainMatcher(nil)
	_, ok := m.MatchRecipient("user@example.com")
	if ok {
		t.Error("expected no match with empty domain configs")
	}
}

// ---- AdvancedMatcher additional coverage ----

func TestAdvancedMatcher_InvalidRegex(t *testing.T) {
	rule := router.AdvancedRule{RecipientRegex: "[invalid"}
	_, err := router.NewAdvancedMatcher([]router.AdvancedRule{rule})
	if err == nil {
		t.Error("expected error for invalid regex")
	}
}

func TestAdvancedMatcher_EmptyRules(t *testing.T) {
	m, err := router.NewAdvancedMatcher(nil)
	if err != nil {
		t.Fatalf("NewAdvancedMatcher(nil): %v", err)
	}
	_, ok := m.Match(&types.EmailEvent{EnvelopeTo: []string{"anyone@example.com"}})
	if ok {
		t.Error("expected no match with empty rules")
	}
}

func TestAdvancedMatcher_NoRegex_HeaderOnly_NoMatch(t *testing.T) {
	rule := router.AdvancedRule{
		HeaderConditions: map[string]string{"X-Custom": "value"},
		Target:           router.TargetWebhook,
	}
	m, _ := router.NewAdvancedMatcher([]router.AdvancedRule{rule})

	// header key missing
	event := &types.EmailEvent{Headers: map[string][]string{}}
	_, ok := m.Match(event)
	if ok {
		t.Error("expected no match when header key is absent")
	}
}

func TestAdvancedMatcher_HeaderValueMismatch(t *testing.T) {
	rule := router.AdvancedRule{
		HeaderConditions: map[string]string{"X-Priority": "urgent"},
		Target:           router.TargetWebhook,
	}
	m, _ := router.NewAdvancedMatcher([]router.AdvancedRule{rule})
	event := &types.EmailEvent{
		Headers: map[string][]string{"X-Priority": {"low"}},
	}
	_, ok := m.Match(event)
	if ok {
		t.Error("expected no match when header value doesn't match")
	}
}

func TestAdvancedMatcher_HeaderCaseInsensitive(t *testing.T) {
	rule := router.AdvancedRule{
		HeaderConditions: map[string]string{"X-Priority": "URGENT"},
		Target:           router.TargetWebhook,
		WebhookURL:       "https://x.com/case",
	}
	m, _ := router.NewAdvancedMatcher([]router.AdvancedRule{rule})
	event := &types.EmailEvent{
		Headers: map[string][]string{"X-Priority": {"urgent"}},
	}
	matched, ok := m.Match(event)
	if !ok {
		t.Fatal("expected case-insensitive header match")
	}
	if matched.WebhookURL != "https://x.com/case" {
		t.Errorf("wrong URL: %q", matched.WebhookURL)
	}
}

func TestAdvancedMatcher_RegexNoRecipients(t *testing.T) {
	rule := router.AdvancedRule{
		RecipientRegex: `^support@example\.com$`,
		Target:         router.TargetWebhook,
	}
	m, _ := router.NewAdvancedMatcher([]router.AdvancedRule{rule})
	// event has no recipients → regex cannot match
	event := &types.EmailEvent{EnvelopeTo: []string{}}
	_, ok := m.Match(event)
	if ok {
		t.Error("expected no match when EnvelopeTo is empty")
	}
}

func TestAdvancedMatcher_RegexMatchesOneOfMultipleRecipients(t *testing.T) {
	rule := router.AdvancedRule{
		RecipientRegex: `^billing@example\.com$`,
		Target:         router.TargetWebhook,
		WebhookURL:     "https://x.com/billing",
	}
	m, _ := router.NewAdvancedMatcher([]router.AdvancedRule{rule})
	event := &types.EmailEvent{
		EnvelopeTo: []string{"other@example.com", "billing@example.com"},
	}
	matched, ok := m.Match(event)
	if !ok {
		t.Fatal("expected match on second recipient")
	}
	if matched.WebhookURL != "https://x.com/billing" {
		t.Errorf("wrong URL: %q", matched.WebhookURL)
	}
}

func TestAdvancedMatcher_NoRegexNoHeaders_AlwaysMatches(t *testing.T) {
	// A rule with no regex and no header conditions matches every event.
	rule := router.AdvancedRule{Target: router.TargetWebhook, WebhookURL: "https://x.com/catch"}
	m, _ := router.NewAdvancedMatcher([]router.AdvancedRule{rule})
	event := &types.EmailEvent{EnvelopeTo: []string{"any@example.com"}}
	_, ok := m.Match(event)
	if !ok {
		t.Error("expected unconditional match")
	}
}

func TestAdvancedMatcher_FirstMatchWins(t *testing.T) {
	rules := []router.AdvancedRule{
		{Target: router.TargetWebhook, WebhookURL: "https://first.com"},
		{Target: router.TargetWebhook, WebhookURL: "https://second.com"},
	}
	m, _ := router.NewAdvancedMatcher(rules)
	event := &types.EmailEvent{}
	matched, ok := m.Match(event)
	if !ok {
		t.Fatal("expected match")
	}
	if matched.WebhookURL != "https://first.com" {
		t.Errorf("expected first rule to win, got %q", matched.WebhookURL)
	}
}
