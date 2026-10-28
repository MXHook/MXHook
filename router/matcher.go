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

// normalizeRecipient lowercases the whole address and strips plus-addressing:
// Task+label@Domain.com → task@domain.com.
//
// Folding the local part is a deliberate policy: RFC 5321 permits
// case-sensitive local parts, but rule patterns are stored lowercased, so
// matching folds the whole address to keep both sides consistent. The
// original-case recipient is preserved upstream for delivery payloads.
func normalizeRecipient(addr string) string {
	addr = strings.ToLower(addr)
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
	if pattern == "*" {
		return true
	}
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
