package smtp_test

import (
	"context"
	"errors"
	"net"
	"net/textproto"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	netsmtp "net/smtp"

	"github.com/mxhook/mxhook/queue"
	mxsmtp "github.com/mxhook/mxhook/smtp"
)

// stubDomainSource is a controllable DomainSource standing in for the durable
// store: it records call counts so cache behaviour is observable, and can be
// switched to a failing mode to exercise the unavailable-store policy.
type stubDomainSource struct {
	mu      sync.Mutex
	domains []string
	err     error
	calls   int32
}

func (s *stubDomainSource) ActiveDomains(context.Context) ([]string, error) {
	atomic.AddInt32(&s.calls, 1)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return nil, s.err
	}
	out := make([]string, len(s.domains))
	copy(out, s.domains)
	return out, nil
}

func (s *stubDomainSource) set(domains []string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.domains, s.err = domains, err
}

func (s *stubDomainSource) Calls() int { return int(atomic.LoadInt32(&s.calls)) }

// newServerWithDomains starts a server backed by a DomainSource and returns it
// alongside its address so tests can drive invalidation directly.
func newServerWithDomains(t *testing.T, cfg mxsmtp.Config, enq queue.Enqueuer, source mxsmtp.DomainSource) (*mxsmtp.Server, string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	cfg.Addr = addr
	server, err := mxsmtp.NewServer(cfg, enq, mxsmtp.WithDomainSource(source))
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	t.Cleanup(func() { _ = server.Close() })

	ready := make(chan struct{})
	go func() {
		close(ready)
		_ = server.ListenAndServe()
	}()
	<-ready
	if !waitPort(addr, 2*time.Second) {
		t.Fatalf("server did not start on %s", addr)
	}
	return server, addr
}

func startServerWithDomains(t *testing.T, cfg mxsmtp.Config, enq queue.Enqueuer, source mxsmtp.DomainSource) string {
	t.Helper()
	_, addr := newServerWithDomains(t, cfg, enq, source)
	return addr
}

func rcptCode(t *testing.T, addr, recipient string) int {
	t.Helper()
	client, err := netsmtp.Dial(addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = client.Close() }()
	if err := client.Mail("sender@example.com"); err != nil {
		t.Fatalf("MAIL: %v", err)
	}
	err = client.Rcpt(recipient)
	if err == nil {
		return 250
	}
	var protoErr *textproto.Error
	if errors.As(err, &protoErr) {
		return protoErr.Code
	}
	t.Fatalf("RCPT %s: unexpected error type %v", recipient, err)
	return 0
}

// A domain registered in the durable store after startup must be accepted
// without restarting the server: this is the whole point of `domain add`.
func TestRcptAcceptsDomainAddedToStoreAfterStartup(t *testing.T) {
	source := &stubDomainSource{}
	cfg := defaultCfg()
	// Keep the cache from masking the registration mid-test; the TTL itself
	// is covered by TestDomainCacheExpiresAfterTTL.
	cfg.DomainCacheTTL = time.Millisecond
	addr := startServerWithDomains(t, cfg, &countingEnqueuer{}, source)

	if code := rcptCode(t, addr, "user@dynamic.example"); code != 550 {
		t.Fatalf("RCPT for unregistered domain = %d, want 550", code)
	}

	source.set([]string{"dynamic.example"}, nil)
	time.Sleep(5 * time.Millisecond)

	if code := rcptCode(t, addr, "user@dynamic.example"); code != 250 {
		t.Fatalf("RCPT after registering domain = %d, want 250 (no restart)", code)
	}
}

// Static configuration remains a floor: operators using --recipient-domain
// keep working even when the durable store knows nothing about the domain.
func TestRcptStillAcceptsStaticallyConfiguredDomain(t *testing.T) {
	source := &stubDomainSource{}
	addr := startServerWithDomains(t, defaultCfg(), &countingEnqueuer{}, source)

	if code := rcptCode(t, addr, "user@example.com"); code != 250 {
		t.Fatalf("RCPT for statically configured domain = %d, want 250", code)
	}
}

// An unknown domain must still be refused permanently once the store has been
// consulted successfully; anything else is an open relay.
func TestRcptRejectsUnknownDomain(t *testing.T) {
	source := &stubDomainSource{}
	source.set([]string{"other.example"}, nil)
	addr := startServerWithDomains(t, defaultCfg(), &countingEnqueuer{}, source)

	if code := rcptCode(t, addr, "user@unknown.example"); code != 550 {
		t.Fatalf("RCPT for unknown domain = %d, want 550", code)
	}
}

// Deleting a domain through the API invalidates the cache, so acceptance stops
// without waiting for the TTL.
func TestInvalidateDomainsDropsRemovedDomain(t *testing.T) {
	source := &stubDomainSource{}
	source.set([]string{"temp.example"}, nil)
	server, addr := newServerWithDomains(t, defaultCfg(), &countingEnqueuer{}, source)

	if code := rcptCode(t, addr, "user@temp.example"); code != 250 {
		t.Fatalf("RCPT before removal = %d, want 250", code)
	}

	source.set(nil, nil)
	server.InvalidateDomains()

	if code := rcptCode(t, addr, "user@temp.example"); code != 550 {
		t.Fatalf("RCPT after removal and invalidation = %d, want 550", code)
	}
}

// Cached results are reused: the store is not queried once per recipient.
func TestDomainLookupIsCached(t *testing.T) {
	source := &stubDomainSource{}
	source.set([]string{"cached.example"}, nil)
	addr := startServerWithDomains(t, defaultCfg(), &countingEnqueuer{}, source)

	for i := 0; i < 5; i++ {
		if code := rcptCode(t, addr, "user@cached.example"); code != 250 {
			t.Fatalf("RCPT %d = %d, want 250", i, code)
		}
	}
	if calls := source.Calls(); calls > 1 {
		t.Fatalf("store consulted %d times for 5 recipients, want a single cached load", calls)
	}
}

// Store unavailable, warm cache: keep serving the last known good set rather
// than failing mail that was previously accepted.
func TestRcptServesStaleCacheWhenStoreUnavailable(t *testing.T) {
	source := &stubDomainSource{}
	source.set([]string{"warm.example"}, nil)
	server, addr := newServerWithDomains(t, defaultCfg(), &countingEnqueuer{}, source)

	if code := rcptCode(t, addr, "user@warm.example"); code != 250 {
		t.Fatalf("RCPT priming cache = %d, want 250", code)
	}

	source.set(nil, errors.New("database unavailable"))
	server.InvalidateDomains() // force a reload attempt against the failing store

	if code := rcptCode(t, addr, "user@warm.example"); code != 250 {
		t.Fatalf("RCPT with warm cache and failing store = %d, want 250 (stale served)", code)
	}
}

// Store unavailable, cold cache: static configuration still answers, so a
// config-only deployment is unaffected by a database outage.
func TestRcptFallsBackToStaticConfigWhenStoreUnavailableAndCacheCold(t *testing.T) {
	source := &stubDomainSource{}
	source.set(nil, errors.New("database unavailable"))
	addr := startServerWithDomains(t, defaultCfg(), &countingEnqueuer{}, source)

	if code := rcptCode(t, addr, "user@example.com"); code != 250 {
		t.Fatalf("RCPT for static domain with failing store = %d, want 250", code)
	}
}

// Store unavailable, cold cache, domain not in static config: the answer is
// unknowable, so reply 451 and let the sender retry rather than bouncing mail
// with a permanent 550.
func TestRcptReturnsTemporaryFailureWhenUndecidable(t *testing.T) {
	source := &stubDomainSource{}
	source.set(nil, errors.New("database unavailable"))
	addr := startServerWithDomains(t, defaultCfg(), &countingEnqueuer{}, source)

	if code := rcptCode(t, addr, "user@dynamic.example"); code != 451 {
		t.Fatalf("RCPT with failing store and cold cache = %d, want 451 (temporary)", code)
	}
}

// A server with no durable source at all keeps the pre-existing static
// behaviour and must not panic.
func TestRcptWithoutDomainSourceUsesStaticConfig(t *testing.T) {
	addr := startServer(t, defaultCfg(), &countingEnqueuer{})

	if code := rcptCode(t, addr, "user@example.com"); code != 250 {
		t.Fatalf("RCPT static-only accepted domain = %d, want 250", code)
	}
	if code := rcptCode(t, addr, "user@dynamic.example"); code != 550 {
		t.Fatalf("RCPT static-only unknown domain = %d, want 550", code)
	}
}

// Cached entries expire so that domains added on another process are picked up
// without an explicit invalidation.
func TestDomainCacheExpiresAfterTTL(t *testing.T) {
	source := &stubDomainSource{}
	cfg := defaultCfg()
	cfg.DomainCacheTTL = 20 * time.Millisecond
	addr := startServerWithDomains(t, cfg, &countingEnqueuer{}, source)

	if code := rcptCode(t, addr, "user@ttl.example"); code != 550 {
		t.Fatalf("RCPT before registration = %d, want 550", code)
	}

	source.set([]string{"ttl.example"}, nil)
	time.Sleep(40 * time.Millisecond)

	if code := rcptCode(t, addr, "user@ttl.example"); code != 250 {
		t.Fatalf("RCPT after TTL expiry = %d, want 250", code)
	}
}
