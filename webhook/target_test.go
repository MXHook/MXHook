package webhook

import (
	"context"
	"net"
	"reflect"
	"strings"
	"testing"

	"github.com/mxhook/mxhook/types"
)

// failingLookup returns a lookup func that always fails with the given error.
func failingLookup(err error) func(context.Context, string) ([]net.IPAddr, error) {
	return func(context.Context, string) ([]net.IPAddr, error) { return nil, err }
}

func TestDeliver_ResolverFailureIsTemporary(t *testing.T) {
	d := NewDeliverer(Config{MaxAttempts: 1, TimeoutSecs: 1})
	d.policy.lookupIP = failingLookup(&net.DNSError{
		Err:         "server misbehaving",
		Name:        "hooks.example.test",
		IsTemporary: true,
	})

	err := d.Deliver(context.Background(), Endpoint{
		URL: "https://hooks.example.test/in", Secret: "s",
	}, &types.EmailEvent{ID: "dns-blip"})
	if err == nil {
		t.Fatal("expected error when resolution fails")
	}
	if !IsTemporary(err) {
		t.Fatalf("resolver failure must be temporary (retried, not dead-lettered), got permanent: %v", err)
	}
}

func TestDeliver_ResolverNotFoundIsTemporary(t *testing.T) {
	d := NewDeliverer(Config{MaxAttempts: 1, TimeoutSecs: 1})
	d.policy.lookupIP = failingLookup(&net.DNSError{
		Err:        "no such host",
		Name:       "hooks.example.test",
		IsNotFound: true,
	})

	err := d.Deliver(context.Background(), Endpoint{
		URL: "https://hooks.example.test/in", Secret: "s",
	}, &types.EmailEvent{ID: "nxdomain"})
	if err == nil {
		t.Fatal("expected error when resolution fails")
	}
	if !IsTemporary(err) {
		t.Fatalf("NXDOMAIN must be temporary (records reappear), got permanent: %v", err)
	}
}

func TestDeliver_ResolvedPrivateTargetIsPermanent(t *testing.T) {
	d := NewDeliverer(Config{MaxAttempts: 1, TimeoutSecs: 1})
	d.policy.lookupIP = func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("10.0.0.8")}}, nil
	}

	err := d.Deliver(context.Background(), Endpoint{
		URL: "https://hooks.example.test/in", Secret: "s",
	}, &types.EmailEvent{ID: "resolved-private"})
	if err == nil {
		t.Fatal("expected error for private-network target")
	}
	if IsTemporary(err) {
		t.Fatalf("private-network policy rejection must be permanent, got temporary: %v", err)
	}
}

// loopbackListener binds an IPv4 loopback listener, accepts connections in
// the background, and returns its port.
func loopbackListener(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
		}
	}()
	_, port, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatalf("split listener address: %v", err)
	}
	return port
}

func TestDialContext_IteratesAddressesOnDialFailure(t *testing.T) {
	port := loopbackListener(t)
	p := targetPolicy{allowPrivate: true, lookupIP: func(context.Context, string) ([]net.IPAddr, error) {
		// First address unreachable (nothing listens on [::1]:port),
		// second is the live listener: delivery must fall through.
		return []net.IPAddr{{IP: net.ParseIP("::1")}, {IP: net.ParseIP("127.0.0.1")}}, nil
	}}

	conn, err := p.transport().DialContext(context.Background(), "tcp", net.JoinHostPort("hooks.example.test", port))
	if err != nil {
		t.Fatalf("dial must iterate to the reachable address, got: %v", err)
	}
	_ = conn.Close()
}

func TestDialContext_FiltersAddressesByRequestedNetwork(t *testing.T) {
	p := targetPolicy{allowPrivate: true, lookupIP: func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("::1")}}, nil
	}}

	_, err := p.transport().DialContext(context.Background(), "tcp4", "hooks.example.test:443")
	if err == nil || !strings.Contains(err.Error(), "no tcp4 addresses") {
		t.Fatalf("tcp4 dial with only IPv6 candidates must report no usable addresses, got: %v", err)
	}
}

func TestUnsafeIP_SpecialPurposeRanges(t *testing.T) {
	unsafe := []string{
		// Long-standing categories.
		"0.0.0.0", "127.0.0.1", "10.0.0.8", "172.16.0.1", "192.168.1.1",
		"169.254.1.1", "224.0.0.1", "::", "::1", "fe80::1", "fc00::1",
		// RFC 6598 carrier-grade NAT (Tailscale-style overlays live here).
		"100.64.0.1", "100.127.255.255",
		// IANA special-purpose registries.
		"192.0.0.10",      // RFC 6890 IETF protocol assignments
		"192.0.2.1",       // RFC 5737 TEST-NET-1
		"198.18.0.1",      // RFC 2544 benchmarking
		"198.19.255.255",  // RFC 2544 benchmarking upper bound
		"198.51.100.7",    // RFC 5737 TEST-NET-2
		"203.0.113.9",     // RFC 5737 TEST-NET-3
		"240.0.0.1",       // RFC 1112 reserved
		"255.255.255.255", // limited broadcast
		"100::1",          // RFC 6666 discard-only
		"2001:db8::1",     // RFC 3849 documentation
	}
	for _, value := range unsafe {
		if !unsafeIP(net.ParseIP(value)) {
			t.Errorf("%s must be classified unsafe", value)
		}
	}
	safe := []string{
		"1.1.1.1", "8.8.8.8", "93.184.216.34",
		"100.128.0.1", // just past the CGNAT /10
		"2606:4700:4700::1111",
	}
	for _, value := range safe {
		if unsafeIP(net.ParseIP(value)) {
			t.Errorf("%s must remain deliverable", value)
		}
	}
}

func TestFilterByNetwork(t *testing.T) {
	v6 := net.ParseIP("2001:db8::1")
	v4 := net.ParseIP("192.0.2.10")
	all := []net.IP{v6, v4}

	if got := filterByNetwork(all, "tcp4"); !reflect.DeepEqual(got, []net.IP{v4}) {
		t.Fatalf("tcp4 filter = %v", got)
	}
	if got := filterByNetwork(all, "tcp6"); !reflect.DeepEqual(got, []net.IP{v6}) {
		t.Fatalf("tcp6 filter = %v", got)
	}
	if got := filterByNetwork(all, "tcp"); !reflect.DeepEqual(got, all) {
		t.Fatalf("tcp filter = %v", got)
	}
}

func TestDeliver_PrivateIPLiteralIsPermanent(t *testing.T) {
	d := NewDeliverer(Config{MaxAttempts: 1, TimeoutSecs: 1})

	err := d.Deliver(context.Background(), Endpoint{
		URL: "https://10.0.0.8/in", Secret: "s",
	}, &types.EmailEvent{ID: "literal-private"})
	if err == nil {
		t.Fatal("expected error for private-network target")
	}
	if IsTemporary(err) {
		t.Fatalf("private-network policy rejection must be permanent, got temporary: %v", err)
	}
}
