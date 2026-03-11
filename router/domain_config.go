package router

import "strings"

// DomainConfig holds routing rules for a single domain.
type DomainConfig struct {
	Domain   string
	Rules    []Rule
	CatchAll *Rule // applied when no specific rule matches
}

// DomainMatcher routes recipient addresses across multiple domain configs.
type DomainMatcher struct {
	domains map[string]DomainConfig
}

// NewDomainMatcher creates a DomainMatcher from a slice of DomainConfig.
func NewDomainMatcher(configs []DomainConfig) *DomainMatcher {
	m := &DomainMatcher{domains: make(map[string]DomainConfig)}
	for _, cfg := range configs {
		m.domains[strings.ToLower(cfg.Domain)] = cfg
	}
	return m
}

// MatchRecipient finds the best matching Rule for a recipient address.
// It tries specific rules first, then catch-all.
func (m *DomainMatcher) MatchRecipient(recipient string) (Rule, bool) {
	domain := recipientDomain(recipient)
	cfg, ok := m.domains[domain]
	if !ok {
		return Rule{}, false
	}

	// try specific rules
	matcher := NewMatcher(cfg.Rules)
	if rule, ok := matcher.Match(recipient); ok {
		return rule, true
	}

	// fall back to catch-all
	if cfg.CatchAll != nil {
		return *cfg.CatchAll, true
	}

	return Rule{}, false
}

func recipientDomain(addr string) string {
	parts := strings.SplitN(addr, "@", 2)
	if len(parts) != 2 {
		return ""
	}
	return strings.ToLower(parts[1])
}
