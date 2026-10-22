package mcpauth

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

// requestTimeout bounds every outbound HTTP request the package makes (token
// exchange, refresh, discovery, DCR). It mirrors the bound used by the MCP
// registry transports.
const requestTimeout = 15 * time.Second

// maxBodyBytes caps response bodies (token responses, metadata documents) so a
// misbehaving endpoint cannot make the server buffer unbounded data.
const maxBodyBytes = 1 << 20 // 1 MiB

// OutboundHTTPClient returns an *http.Client guarded against SSRF for the given
// remote URL. It mirrors apps/server/domain/mcpregistry/outbound.go so a
// caller-supplied (or discovered) endpoint cannot make the server fetch internal
// infrastructure:
//
//   - DialContext resolves the target and re-checks it against the blocked
//     address ranges immediately before the socket opens (DNS-rebind defence),
//     and only when no egress proxy is configured for the target.
//   - CheckRedirect validates every redirect hop with the same guard and caps
//     the chain length.
//
// It is used for the MCP resource URL and as the default for discovery when no
// HTTP client is injected.
func OutboundHTTPClient(rawURL string) *http.Client {
	tr, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		// Defensive: never panic on an unexpected default transport; fall back
		// to a plain client (pre-flight URL validation still applies).
		return &http.Client{Timeout: requestTimeout, CheckRedirect: safeRedirectPolicy}
	}
	tr = tr.Clone()

	if req, err := http.NewRequest(http.MethodGet, rawURL, nil); err == nil {
		if proxy, perr := http.ProxyFromEnvironment(req); perr == nil && proxy == nil {
			tr.DialContext = guardedDialContext(tr.DialContext)
		}
	}

	return &http.Client{
		Transport:     tr,
		Timeout:       requestTimeout,
		CheckRedirect: safeRedirectPolicy,
	}
}

// safeRedirectPolicy validates each redirect hop with the shared SSRF guard and
// caps the chain length.
func safeRedirectPolicy(req *http.Request, via []*http.Request) error {
	if len(via) >= 5 {
		return errors.New("stopped after 5 redirects")
	}
	if err := validateURL(req.URL.String()); err != nil {
		return fmt.Errorf("redirect blocked: %w", err)
	}
	return nil
}

// guardedDialContext wraps a dialer so every connection target is resolved and
// checked against the blocked-address guard before the socket is opened. A
// hostname is resolved exactly once: if every returned address is public the
// connection is made to one of those validated addresses directly, so a DNS
// rebind between validation and connect cannot slip an internal address in.
func guardedDialContext(next func(ctx context.Context, network, address string) (net.Conn, error)) func(ctx context.Context, network, address string) (net.Conn, error) {
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
			if blockedAddr(addr) {
				return nil, fmt.Errorf("refusing to connect to disallowed address %s", host)
			}
			return next(ctx, network, address)
		}

		ips, err := lookupHostIPs(ctx, host)
		if err != nil {
			return nil, fmt.Errorf("resolving %q: %w", host, err)
		}
		if len(ips) == 0 {
			return nil, fmt.Errorf("%q did not resolve to any address", host)
		}

		dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
		var lastErr error
		for _, ip := range ips {
			if blockedAddr(ip) {
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

// blockedAddr reports whether ip lies in a range that an outbound OAuth/MCP
// request must never reach: loopback, private, link-local, unique-local,
// multicast, unspecified, CGNAT, and reserved ranges are all rejected.
func blockedAddr(ip netip.Addr) bool {
	if !ip.IsValid() {
		return true
	}
	ip = ip.Unmap()
	if ip.IsLoopback() ||
		ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
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

// isInternalHostname reports whether a hostname names only private
// infrastructure. It is a small denylist because validateURL deliberately does
// not perform DNS resolution on the static path.
func isInternalHostname(host string) bool {
	switch host {
	case "localhost", "metadata", "metadata.google.internal", "instance-data":
		return true
	}
	return strings.HasSuffix(host, ".localhost") ||
		strings.HasSuffix(host, ".local") ||
		strings.HasSuffix(host, ".internal")
}

// validateURL rejects remote URLs that the server must not connect to:
// non-http(s) schemes, userinfo, literal IPs in blocked ranges, and
// known-internal hostnames. It does NOT resolve hostnames (resolution happens
// at dial time via guardedDialContext).
func validateURL(raw string) error {
	if strings.TrimSpace(raw) == "" {
		return fmt.Errorf("remote URL is empty")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid remote URL %q: %w", raw, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("remote URL scheme %q not allowed (only http/https)", u.Scheme)
	}
	if u.User != nil {
		return fmt.Errorf("remote URL must not contain userinfo")
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("remote URL %q has no host", raw)
	}
	if addr, perr := netip.ParseAddr(host); perr == nil {
		if blockedAddr(addr) {
			return fmt.Errorf("remote URL host %s is in a disallowed address range", host)
		}
		return nil
	}
	if isInternalHostname(strings.ToLower(host)) {
		return fmt.Errorf("remote URL host %q is a disallowed internal hostname", host)
	}
	return nil
}

// lookupHostIPs resolves a hostname to its addresses for SSRF validation. It is
// a package variable so unit tests can substitute a deterministic resolver and
// avoid real DNS.
var lookupHostIPs = func(ctx context.Context, host string) ([]netip.Addr, error) {
	return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
}
