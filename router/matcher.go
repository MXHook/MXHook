package router

import "strings"

// Matcher routes recipient addresses to Rules.
type Matcher struct {
	rules []Rule
}

// NewMatcher creates a Matcher from a set of rules.
func NewMatcher(rules []Rule) *Matcher {
	return &Matcher{rules: rules}
}

// Match returns the first matching Rule for a recipient address.
func (m *Matcher) Match(recipient string) (Rule, bool) {
	normalized := normalizeRecipient(recipient)
	for _, r := range m.rules {
		if matches(r.RecipientPattern, normalized) {
			return r, true
		}
	}
	return Rule{}, false
}

// normalizeRecipient strips plus-addressing: task+label@domain.com → task@domain.com
func normalizeRecipient(addr string) string {
	parts := strings.SplitN(addr, "@", 2)
	if len(parts) != 2 {
		return addr
	}
	local := parts[0]
	if i := strings.Index(local, "+"); i >= 0 {
		local = local[:i]
	}
	return local + "@" + parts[1]
}

func matches(pattern, addr string) bool {
	if pattern == addr {
		return true
	}
	// Wildcard: *@domain.com
	if strings.HasPrefix(pattern, "*@") {
		domain := pattern[2:]
		return strings.HasSuffix(addr, "@"+domain)
	}
	return false
}
