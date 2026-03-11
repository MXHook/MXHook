package router

import (
	"regexp"
	"strings"

	"github.com/mxhook/mxhook/internal/types"
)

// AdvancedRule extends Rule with regex recipient matching and header conditions.
// This is an EE feature.
type AdvancedRule struct {
	RecipientRegex   string            // compiled to regexp
	HeaderConditions map[string]string // header name → required value (exact match)
	Target           Target
	WebhookURL       string
	WebhookSecret    string
	Priority         int
}

// compiledRule is an AdvancedRule with pre-compiled regex.
type compiledRule struct {
	rule  AdvancedRule
	regex *regexp.Regexp
}

// AdvancedMatcher matches EmailEvents against AdvancedRules.
type AdvancedMatcher struct {
	rules []compiledRule
}

// NewAdvancedMatcher creates an AdvancedMatcher, compiling all regex patterns.
func NewAdvancedMatcher(rules []AdvancedRule) (*AdvancedMatcher, error) {
	compiled := make([]compiledRule, 0, len(rules))
	for _, r := range rules {
		cr := compiledRule{rule: r}
		if r.RecipientRegex != "" {
			rx, err := regexp.Compile(r.RecipientRegex)
			if err != nil {
				return nil, err
			}
			cr.regex = rx
		}
		compiled = append(compiled, cr)
	}
	return &AdvancedMatcher{rules: compiled}, nil
}

// Match returns the first AdvancedRule matching the event.
func (m *AdvancedMatcher) Match(event *types.EmailEvent) (AdvancedRule, bool) {
	for _, cr := range m.rules {
		if cr.matchesEvent(event) {
			return cr.rule, true
		}
	}
	return AdvancedRule{}, false
}

func (cr *compiledRule) matchesEvent(event *types.EmailEvent) bool {
	// Regex recipient check
	if cr.regex != nil {
		matched := false
		for _, to := range event.EnvelopeTo {
			if cr.regex.MatchString(to) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}

	// Header condition check
	for k, want := range cr.rule.HeaderConditions {
		vals, ok := event.Headers[k]
		if !ok {
			return false
		}
		found := false
		for _, v := range vals {
			if strings.EqualFold(v, want) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	return true
}
