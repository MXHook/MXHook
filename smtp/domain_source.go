package smtp

import (
	"context"
	"strings"
	"sync"
	"time"
)

// DomainSource supplies the set of recipient domains registered through the
// control plane. It is deliberately narrower than the API repository so the
// SMTP package does not depend on the API or state packages.
type DomainSource interface {
	ActiveDomains(ctx context.Context) ([]string, error)
}

// defaultDomainCacheTTL bounds how long a loaded domain set is reused. Reading
// the durable store on every RCPT TO would put a database round-trip on the
// SMTP hot path; a short TTL keeps `domain add` taking effect promptly even
// when the writer is a different process, where in-process invalidation cannot
// reach. This mirrors the rule cache in internal/runtime.
const defaultDomainCacheTTL = 5 * time.Second

// domainLookupTimeout bounds a single store read so a wedged database cannot
// hold an SMTP session open.
const domainLookupTimeout = 2 * time.Second

// domainResolver answers "is this domain accepted?" from the union of the
// static configuration and the durable store, with the store's contribution
// cached.
//
// Availability policy, in order:
//
//  1. Static configuration always answers yes for the domains it lists. It is
//     a floor, never overridden by the store, so an operator who configured
//     --recipient-domain keeps working during a database outage.
//  2. A fresh cache answers from the store.
//  3. If the store is unreachable but a previously loaded set is cached, that
//     stale set is served. Mail that was accepted a moment ago keeps being
//     accepted rather than failing on a transient blip.
//  4. If the store is unreachable and nothing has ever been cached, the answer
//     is unknowable. The resolver reports that explicitly so the caller can
//     reply 451 and have the sender retry. It never fails open: an unknown
//     domain is never accepted on the strength of a failed lookup.
type domainResolver struct {
	static map[string]struct{}
	source DomainSource
	ttl    time.Duration
	now    func() time.Time

	mu       sync.Mutex
	cached   map[string]struct{}
	cachedAt time.Time
}

func newDomainResolver(static []string, source DomainSource, ttl time.Duration) *domainResolver {
	set := make(map[string]struct{}, len(static))
	for _, domain := range static {
		set[normalizeDomain(domain)] = struct{}{}
	}
	if ttl <= 0 {
		ttl = defaultDomainCacheTTL
	}
	return &domainResolver{static: set, source: source, ttl: ttl, now: time.Now}
}

// invalidate marks the cached set stale so the next lookup reloads it. The set
// itself is retained: if that reload fails because the store is unreachable,
// the last known good answer is still better than refusing mail.
func (r *domainResolver) invalidate() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cachedAt = time.Time{}
}

// allows reports whether the domain is accepted, and whether the answer is
// decidable. A false, false result means the store could not be consulted and
// nothing else vouches for the domain.
func (r *domainResolver) allows(ctx context.Context, domain string) (allowed bool, decidable bool) {
	domain = normalizeDomain(domain)
	if _, ok := r.static[domain]; ok {
		return true, true
	}
	if r.source == nil {
		// No durable store wired: static configuration is the whole truth,
		// and it has already answered.
		return false, true
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.cachedAt.IsZero() && r.now().Sub(r.cachedAt) < r.ttl {
		_, ok := r.cached[domain]
		return ok, true
	}

	lookupCtx, cancel := context.WithTimeout(ctx, domainLookupTimeout)
	defer cancel()
	domains, err := r.source.ActiveDomains(lookupCtx)
	if err != nil {
		if r.cached != nil {
			// Stale but known good: better than bouncing mail we accepted a
			// moment ago.
			_, ok := r.cached[domain]
			return ok, true
		}
		return false, false
	}

	set := make(map[string]struct{}, len(domains))
	for _, d := range domains {
		set[normalizeDomain(d)] = struct{}{}
	}
	r.cached, r.cachedAt = set, r.now()
	_, ok := set[domain]
	return ok, true
}

func normalizeDomain(domain string) string {
	return strings.ToLower(strings.TrimRight(strings.TrimSpace(domain), "."))
}
