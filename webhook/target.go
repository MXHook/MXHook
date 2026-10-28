package webhook

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type targetPolicy struct {
	allowHTTP    bool
	allowPrivate bool
	// lookupIP overrides DNS resolution; nil means net.DefaultResolver.
	lookupIP func(ctx context.Context, host string) ([]net.IPAddr, error)
}

func (p targetPolicy) validateRawURL(ctx context.Context, raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("webhook: invalid URL: %w", err)
	}
	return p.validateURL(ctx, parsed)
}

func (p targetPolicy) validateURL(ctx context.Context, target *url.URL) error {
	if target == nil || target.Hostname() == "" || target.User != nil {
		return errors.New("webhook: URL must contain a host and no user information")
	}
	switch target.Scheme {
	case "https":
	case "http":
		if !p.allowHTTP {
			return errors.New("webhook: HTTPS is required")
		}
	default:
		return fmt.Errorf("webhook: unsupported URL scheme %q", target.Scheme)
	}
	_, err := p.resolve(ctx, target.Hostname())
	return err
}

func (p targetPolicy) transport() *http.Transport {
	return &http.Transport{
		Proxy:                 nil,
		ForceAttemptHTTP2:     true,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, fmt.Errorf("webhook: split target address: %w", err)
			}
			// resolve applies the per-address unsafe-IP policy; only the
			// addresses it vetted may ever be dialed (SSRF pinning).
			addresses, err := p.resolve(ctx, host)
			if err != nil {
				return nil, err
			}
			candidates := filterByNetwork(addresses, network)
			if len(candidates) == 0 {
				return nil, fmt.Errorf("webhook: target %s has no %s addresses", host, network)
			}
			dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
			var lastErr error
			for _, candidate := range candidates {
				conn, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(candidate.String(), port))
				if dialErr == nil {
					return conn, nil
				}
				lastErr = dialErr
				if ctx.Err() != nil {
					break
				}
			}
			return nil, lastErr
		},
	}
}

func (p targetPolicy) resolve(ctx context.Context, host string) ([]net.IP, error) {
	host = strings.TrimSuffix(host, ".")
	if host == "" {
		return nil, errors.New("webhook: target host is empty")
	}
	if parsed := net.ParseIP(host); parsed != nil {
		if !p.allowPrivate && unsafeIP(parsed) {
			return nil, fmt.Errorf("webhook: target address %s is not public", parsed)
		}
		return []net.IP{parsed}, nil
	}
	lookup := p.lookupIP
	if lookup == nil {
		lookup = net.DefaultResolver.LookupIPAddr
	}
	resolved, err := lookup(ctx, host)
	if err != nil {
		// Resolution failures (timeouts, SERVFAIL, NXDOMAIN) are resolver I/O
		// blips, not policy rejections: mark them temporary so deliveries are
		// retried instead of dead-lettered, matching dial-time behavior.
		return nil, &deliveryError{err: fmt.Errorf("webhook: resolve target: %w", err), temporary: true}
	}
	if len(resolved) == 0 {
		return nil, errors.New("webhook: target has no addresses")
	}
	addresses := make([]net.IP, 0, len(resolved))
	for _, address := range resolved {
		if !p.allowPrivate && unsafeIP(address.IP) {
			return nil, fmt.Errorf("webhook: target address %s is not public", address.IP)
		}
		addresses = append(addresses, address.IP)
	}
	return addresses, nil
}

// filterByNetwork keeps only the addresses dialable on the requested
// network: IPv4 for tcp4, IPv6 for tcp6, everything otherwise.
func filterByNetwork(addresses []net.IP, network string) []net.IP {
	var wantV4 bool
	switch network {
	case "tcp4", "udp4", "ip4":
		wantV4 = true
	case "tcp6", "udp6", "ip6":
		wantV4 = false
	default:
		return addresses
	}
	filtered := make([]net.IP, 0, len(addresses))
	for _, address := range addresses {
		if (address.To4() != nil) == wantV4 {
			filtered = append(filtered, address)
		}
	}
	return filtered
}

// specialPurposeNetworks are RFC 6598 and IANA special-purpose ranges that
// net.IP's category predicates do not cover but must never be webhook
// targets: they are unroutable on the public internet or, for CGNAT,
// reachable overlay/provider-internal space.
var specialPurposeNetworks = mustParseCIDRs(
	"100.64.0.0/10",   // RFC 6598 carrier-grade NAT (incl. Tailscale overlays)
	"192.0.0.0/24",    // RFC 6890 IETF protocol assignments
	"192.0.2.0/24",    // RFC 5737 documentation TEST-NET-1
	"198.18.0.0/15",   // RFC 2544 benchmarking
	"198.51.100.0/24", // RFC 5737 documentation TEST-NET-2
	"203.0.113.0/24",  // RFC 5737 documentation TEST-NET-3
	"240.0.0.0/4",     // RFC 1112 reserved, includes 255.255.255.255 broadcast
	"100::/64",        // RFC 6666 discard-only
	"2001:db8::/32",   // RFC 3849 documentation
)

func mustParseCIDRs(values ...string) []*net.IPNet {
	networks := make([]*net.IPNet, 0, len(values))
	for _, value := range values {
		_, network, err := net.ParseCIDR(value)
		if err != nil {
			panic(fmt.Sprintf("webhook: invalid special-purpose CIDR %q: %v", value, err))
		}
		networks = append(networks, network)
	}
	return networks
}

func unsafeIP(address net.IP) bool {
	if address.IsUnspecified() || address.IsLoopback() || address.IsPrivate() ||
		address.IsLinkLocalUnicast() || address.IsLinkLocalMulticast() || address.IsMulticast() {
		return true
	}
	for _, network := range specialPurposeNetworks {
		if network.Contains(address) {
			return true
		}
	}
	return false
}
