// Package outbound provides the shared SSRF guard for every outbound HTTP fetch
// the server performs against a caller- or config-supplied URL. It is the single
// home for URL validation and guarded HTTP client construction so that any
// feature that fetches arbitrary remote content (MCP registry, web scraper,
// future connectors) applies one, consistently-tested policy.
package outbound

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

// Policy controls which addresses an outbound fetch may reach.
//
// The zero value (see DefaultPolicy) is the production-strict policy: only
// public internet addresses are reachable. The two escape hatches exist for
// test/dev and for operators who deliberately host content in private space:
//
//   - AllowedHosts whitelists specific hostnames (exact, or a "*." suffix for
//     every subdomain) regardless of where they resolve.
//   - AllowPrivate permits loopback and RFC1918/ULA private addresses, so a
//     local test server can be crawled during development.
//
// Neither escape hatch relaxes the always-blocked ranges: link-local
// (including the cloud metadata address 169.254.169.254 and its hostnames),
// multicast, unspecified, CGNAT, and reserved space stay unreachable. The
// metadata endpoint is the dominant SSRF target and must never be opened by an
// operator's convenience switch.
type Policy struct {
	// AllowedHosts lists hostnames that are always permitted, even when they
	// resolve to private or internal space. Each entry is matched
	// case-insensitively as an exact host, or as "*." followed by a suffix
	// ("*.example.com" matches any subdomain of example.com but not the bare
	// domain). This is an operator-trusted allowlist: use it for a known
	// internal wiki, not for broad suffixes.
	AllowedHosts []string

	// AllowPrivate permits loopback and RFC1918/ULA private addresses. It is a
	// test/dev escape hatch; link-local/metadata, multicast, unspecified,
	// CGNAT, and reserved ranges remain rejected regardless.
	AllowPrivate bool
}

// DefaultPolicy returns the production-strict policy: no allowed hosts, private
// addresses blocked.
func DefaultPolicy() Policy { return Policy{} }

// LookupHostIPs resolves a hostname to its addresses for SSRF validation. It is
// a package variable so tests can substitute a deterministic resolver and avoid
// real DNS.
var LookupHostIPs = func(ctx context.Context, host string) ([]netip.Addr, error) {
	return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
}

// IsBlockedAddr reports whether ip lies in an address range that an outbound
// fetch must never reach: loopback, private, link-local, unique-local,
// multicast, unspecified, CGNAT, and reserved ranges are all rejected. This is
// the strict (production) classification; Policy.AllowPrivate relaxes only the
// loopback/private portion when enabled.
func IsBlockedAddr(ip netip.Addr) bool {
	return blockedFor(Policy{}, ip)
}

// IsInternalHostname reports whether a hostname names only private
// infrastructure. It is a small denylist for the install-time registry guard,
// which deliberately does not perform DNS resolution.
func IsInternalHostname(host string) bool {
	switch host {
	case "localhost", "metadata", "metadata.google.internal", "instance-data":
		return true
	}
	return strings.HasSuffix(host, ".localhost") ||
		strings.HasSuffix(host, ".local") ||
		strings.HasSuffix(host, ".internal")
}

// ParseBlockedHost reports whether host is a literal IP address that falls in a
// blocked range. It returns false for hostnames (no DNS is performed here) and
// for public literal IPs, so callers that must not resolve DNS can still reject
// the dominant literal-IP SSRF payloads (e.g. http://169.254.169.254/...).
func ParseBlockedHost(host string) bool {
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	return IsBlockedAddr(addr)
}

// alwaysBlocked reports whether ip is in a range that is rejected under every
// policy, including the test/dev AllowPrivate escape hatch. Loopback and
// RFC1918/ULA private space are intentionally NOT listed here: they are the
// ranges AllowPrivate exists to relax.
func alwaysBlocked(ip netip.Addr) bool {
	if !ip.IsValid() {
		return true
	}
	ip = ip.Unmap()
	if ip.IsLinkLocalUnicast() || // 169.254.0.0/16, fe80::/10 (incl. cloud metadata)
		ip.IsLinkLocalMulticast() || // 224.0.0.0/24, ff02::/16
		ip.IsInterfaceLocalMulticast() ||
		ip.IsMulticast() ||
		ip.IsUnspecified() {
		return true
	}
	if ip.Is4() {
		b := ip.As4()
		// 0.0.0.0/8 ("this network"), 100.64.0.0/10 (CGNAT), 240.0.0.0/4 (reserved).
		if b[0] == 0 {
			return true
		}
		if b[0] == 100 && b[1] >= 64 && b[1] <= 127 {
			return true
		}
		if b[0] >= 240 {
			return true
		}
	}
	return false
}

// blockedFor reports whether ip is rejected under pol. The always-blocked
// ranges apply under every policy; loopback/private are relaxed only when
// pol.AllowPrivate is set.
func blockedFor(pol Policy, ip netip.Addr) bool {
	if alwaysBlocked(ip) {
		return true
	}
	if pol.AllowPrivate {
		return false
	}
	ip = ip.Unmap()
	return ip.IsLoopback() || ip.IsPrivate()
}

// permittedFor is the single dial-and-preflight decision: an address is
// permitted when the policy allows it. An AllowedHosts entry (or AllowPrivate)
// relaxes the loopback/private block, but the always-blocked ranges — link-local
// including the cloud metadata endpoint, multicast, unspecified, CGNAT, and
// reserved space — are permitted by no policy.
func permittedFor(pol Policy, host string, ip netip.Addr) bool {
	if alwaysBlocked(ip) {
		return false
	}
	if hostAllowed(strings.ToLower(host), pol.AllowedHosts) || pol.AllowPrivate {
		return true
	}
	ip = ip.Unmap()
	return !ip.IsLoopback() && !ip.IsPrivate()
}

// hostAllowed reports whether host matches an entry in pol.AllowedHosts. Entries
// are matched case-insensitively as an exact host or a "*." wildcard prefix
// covering every subdomain of the suffix.
func hostAllowed(host string, allowed []string) bool {
	for _, a := range allowed {
		a = strings.ToLower(strings.TrimSpace(a))
		if a == "" {
			continue
		}
		if suffix, ok := strings.CutPrefix(a, "*."); ok {
			// "*." requires at least one label before the suffix.
			if strings.HasSuffix(host, "."+suffix) {
				return true
			}
			continue
		}
		if host == a {
			return true
		}
	}
	return false
}

// ValidateURL applies the shared SSRF guard to a raw URL before any outbound
// connection is opened. It rejects non-http(s) schemes, userinfo, literal
// blocked IPs, known-internal hostnames, and — unlike the install-time registry
// check — resolves hostnames and rejects the URL if any resolved address falls
// in a blocked range (closing the hostname-based SSRF gap). AllowedHosts and
// AllowPrivate are honoured as described on Policy.
func ValidateURL(ctx context.Context, raw string, pol Policy) error {
	if strings.TrimSpace(raw) == "" {
		return errors.New("URL is empty")
	}

	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid URL %q: %w", raw, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("URL scheme %q not allowed (only http/https)", u.Scheme)
	}
	if u.User != nil {
		return errors.New("URL must not contain userinfo")
	}

	host := strings.ToLower(u.Hostname())
	if host == "" {
		return fmt.Errorf("URL %q has no host", raw)
	}

	// Literal IP: classify directly, no DNS involved. An AllowedHosts entry does
	// not rescue an always-blocked address (the metadata endpoint and other
	// reserved ranges stay unreachable under every policy).
	if addr, perr := netip.ParseAddr(host); perr == nil {
		if !permittedFor(pol, host, addr) {
			return fmt.Errorf("URL host %s is in a disallowed address range", host)
		}
		return nil
	}

	// An allow-listed hostname is trusted by name: no DNS pre-flight. The dial
	// guard still resolves the target and refuses the always-blocked ranges —
	// but only when it is actually installed, which is when no egress proxy is
	// configured for the target. Behind a proxy the dial goes to the proxy (often
	// a private address) and this name check is the only guard, so the always-
	// blocked ranges are not re-checked on that path.
	if hostAllowed(host, pol.AllowedHosts) {
		return nil
	}

	// Hostname: reject known-internal names (unless private is allowed, in which
	// case the resolved addresses below are checked against the relaxed policy).
	if !pol.AllowPrivate && IsInternalHostname(host) {
		return fmt.Errorf("URL host %q is a disallowed internal hostname", host)
	}

	addrs, err := LookupHostIPs(ctx, host)
	if err != nil {
		return fmt.Errorf("cannot resolve URL host %q: %w", host, err)
	}
	if len(addrs) == 0 {
		return fmt.Errorf("URL host %q did not resolve to any address", host)
	}
	for _, addr := range addrs {
		if !permittedFor(pol, host, addr) {
			return fmt.Errorf("URL host %q resolves to disallowed address %s", host, addr)
		}
	}
	return nil
}

// NewClient returns a guarded *http.Client for fetching rawURL under pol. Two
// layers of defence sit on top of the pre-flight ValidateURL check every fetch
// entry point runs:
//
//   - DialContext: the target is resolved and re-checked against the blocked
//     address ranges immediately before the socket is opened, so a DNS rebind
//     between validation and connect cannot reach internal space. It is only
//     installed when no egress proxy is configured for the target: behind a
//     proxy the dial goes to the proxy (often a private address) and the target
//     itself is covered by the pre-flight check instead.
//   - CheckRedirect: every redirect target is validated with the same policy, so
//     a public URL cannot bounce the server into internal space. Redirects are
//     also capped.
//
// The returned transport is never nil. The MCP registry wraps this client's
// transport with its own authStatusTransport (mapping MCP 401/403 responses to
// a typed error); that mapping is deliberately not part of this package so the
// guard stays transport-agnostic.
func NewClient(rawURL string, pol Policy) *http.Client {
	base, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		// Defensive: never panic on an unexpected default transport; fall back
		// to the default transport (pre-flight validation still applies).
		return &http.Client{
			Transport:     http.DefaultTransport,
			CheckRedirect: redirectPolicy(pol),
		}
	}
	tr := base.Clone()

	if req, err := http.NewRequest(http.MethodGet, rawURL, nil); err == nil {
		if proxy, perr := http.ProxyFromEnvironment(req); perr == nil && proxy == nil {
			tr.DialContext = GuardedDialContext(tr.DialContext, pol)
		}
	}

	return &http.Client{
		Transport:     tr,
		CheckRedirect: redirectPolicy(pol),
	}
}

// NewProbeClient returns the same SSRF-guarded client as NewClient. It exists
// so callers whose protocol needs to observe 401/403 as ordinary responses
// (rather than as a typed auth rejection) can express that intent at the call
// site: the MCP registry wraps NewClient's transport with authStatusTransport
// and uses NewProbeClient for its OAuth discovery probe instead.
func NewProbeClient(rawURL string, pol Policy) *http.Client {
	return NewClient(rawURL, pol)
}

// redirectPolicy validates each redirect hop with the shared SSRF guard and
// caps the chain length.
func redirectPolicy(pol Policy) func(*http.Request, []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("stopped after 5 redirects")
		}
		if err := ValidateURL(req.Context(), req.URL.String(), pol); err != nil {
			return fmt.Errorf("redirect blocked: %w", err)
		}
		return nil
	}
}

// GuardedDialContext wraps a dialer so every connection target is resolved and
// checked against the blocked-address guard before the socket is opened.
//
// A hostname is resolved exactly once: if every returned address is allowed the
// connection is made to one of those validated addresses directly, so a DNS
// rebind between validation and connect cannot slip an internal address in.
func GuardedDialContext(next func(ctx context.Context, network, address string) (net.Conn, error), pol Policy) func(ctx context.Context, network, address string) (net.Conn, error) {
	if next == nil {
		d := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
		next = d.DialContext
	}
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}

		// Literal IP: classify directly, no DNS involved.
		if addr, perr := netip.ParseAddr(host); perr == nil {
			if !permittedFor(pol, host, addr) {
				return nil, fmt.Errorf("refusing to connect to disallowed address %s", host)
			}
			return next(ctx, network, address)
		}

		ips, err := LookupHostIPs(ctx, host)
		if err != nil {
			return nil, fmt.Errorf("resolving %q: %w", host, err)
		}
		if len(ips) == 0 {
			return nil, fmt.Errorf("%q did not resolve to any address", host)
		}

		dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
		var lastErr error
		for _, ip := range ips {
			if !permittedFor(pol, host, ip) {
				return nil, fmt.Errorf("refusing to connect to %q: resolves to disallowed address %s", host, ip)
			}
			conn, derr := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
			if derr == nil {
				return conn, nil
			}
			lastErr = derr
		}
		return nil, lastErr
	}
}
