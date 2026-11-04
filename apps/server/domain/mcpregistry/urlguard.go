package mcpregistry

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/emergent-company/emergent.memory/internal/outbound"
)

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
	if outbound.ParseBlockedHost(host) {
		return fmt.Errorf("remote URL host %s is in a disallowed address range", host)
	}
	if outbound.IsInternalHostname(strings.ToLower(host)) {
		return fmt.Errorf("remote URL host %q is a disallowed internal hostname", host)
	}
	return nil
}

// validateOutboundFetchURL is the shared SSRF guard for every outbound fetch the
// server performs against a caller-supplied MCP server URL: manual create and
// update, the save-less connection probe (POST /api/admin/mcp-servers/test),
// stored-server inspect, and registry installs (via the transports).
//
// It applies the same static checks as validateRegistryRemoteURL (http/https
// only, no userinfo, no literal blocked IP, no known-internal hostname) and then
// — unlike that install-time guard, which deliberately skips DNS — resolves
// hostnames and rejects the URL if any resolved address falls in a blocked
// range. This closes the hostname-based SSRF gap (e.g. a public-looking name
// whose A record points at 169.254.169.254 or an RFC1918 host) that literal-only
// validation misses.
//
// The implementation lives in internal/outbound so the web data source (and
// any future connector) shares one tested policy; this wrapper fixes the MCP
// registry to the production-strict default policy.
func validateOutboundFetchURL(ctx context.Context, raw string) error {
	return outbound.ValidateURL(ctx, raw, outbound.DefaultPolicy())
}
