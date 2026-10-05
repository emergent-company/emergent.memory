package mcpregistry

import (
	"context"
	"fmt"
	"net/netip"
	"net/url"
	"strings"
)

// blockedAddr reports whether ip lies in an address range that a
// registry-supplied remote URL must never reach. Installing a registry entry
// makes the server open an outbound MCP connection to a third-party-controlled
// URL, so loopback, private, link-local, unique-local, multicast, unspecified,
// CGNAT, and reserved ranges are all rejected (SSRF guard).
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
// infrastructure. It is a small denylist because validateRegistryRemoteURL
// deliberately does not perform DNS resolution (see its doc comment).
func isInternalHostname(host string) bool {
	switch host {
	case "localhost", "metadata", "metadata.google.internal", "instance-data":
		return true
	}
	return strings.HasSuffix(host, ".localhost") ||
		strings.HasSuffix(host, ".local") ||
		strings.HasSuffix(host, ".internal")
}

// validateRegistryRemoteURL rejects remote URLs that the server must not
// connect to on behalf of a registry entry: non-http(s) schemes, userinfo,
// literal IPs in loopback/private/link-local/ULA/metadata/CGNAT/reserved
// ranges, and known-internal hostnames.
//
// It deliberately does NOT resolve hostnames with DNS. Resolution at install
// time is environment-dependent: behind an egress proxy every external name
// resolves to the proxy's own (often private) address, so a resolve-and-block
// check would reject every legitimate public remote. DNS-rebinding /
// hostname-based SSRF and HTTP redirect following are tracked as follow-up
// hardening; the dominant literal-IP SSRF payloads (e.g.
// http://169.254.169.254/...) are blocked here.
func validateRegistryRemoteURL(_ context.Context, raw string) error {
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

	// Literal IP: classify directly, no DNS.
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
