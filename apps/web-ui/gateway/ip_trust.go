package main

// Client-IP trust model, mirroring apps/server/internal/proxytrust.
//
// The gateway is its own Go module and its image is built from this directory
// alone with GOWORK=off, so it cannot import the server's implementation; the
// ~60 lines are mirrored instead. Keep the two in step — both carry the same
// contract tests (ip_trust_test.go here, proxytrust_test.go there).
//
// Echo v4's default extractor trusts loopback, private and link-local peers,
// which is the trust model behind GHSA-246p-cpwv-v3jq (rate-limiter bypass via
// spoofed X-Forwarded-For) and GHSA-99jh-6h7p-pp36. This helper replaces it
// with an explicit one: X-Forwarded-For is honoured only for peers listed in
// TRUSTED_PROXY_CIDRS, and an unset list fails closed to the socket peer.

import (
	"fmt"
	"net"
	"strings"

	"github.com/labstack/echo/v4"
)

// trustedProxyEnvVar names the environment variable holding the trusted proxy
// CIDRs (comma-separated). Empty means no peer is trusted and forwarded
// headers are ignored.
const trustedProxyEnvVar = "TRUSTED_PROXY_CIDRS"

// ipTrustDirectMode is the startup-log description of the fail-closed default.
const ipTrustDirectMode = "direct: no forwarded header trusted; set " + trustedProxyEnvVar +
	" to the proxy CIDR(s) if a proxy fronts this service"

// parseTrustedProxyCIDRs validates a TRUSTED_PROXY_CIDRS-style list. Entries
// are trimmed and empty ones dropped; a malformed entry is an error rather than
// being ignored, so a typo cannot silently change the trust model.
func parseTrustedProxyCIDRs(raw []string) ([]*net.IPNet, error) {
	ranges := make([]*net.IPNet, 0, len(raw))
	for _, entry := range raw {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		_, ipNet, err := net.ParseCIDR(entry)
		if err != nil {
			return nil, fmt.Errorf("%s: invalid CIDR %q: %w", trustedProxyEnvVar, entry, err)
		}
		ranges = append(ranges, ipNet)
	}
	return ranges, nil
}

// newIPExtractor returns the echo IP extractor for the configured trusted proxy
// CIDRs, plus a one-line description for the startup log.
//
// With no trusted CIDRs it returns echo.ExtractIPDirect(), which ignores every
// request header and reports the socket peer. With trusted CIDRs it returns
// echo.ExtractIPFromXFFHeader with all implicit trust disabled, so the resolved
// address is the right-most hop that is not a configured proxy — the real
// client as appended by the outermost trusted proxy. (echo falls back to the
// left-most XFF element only when every hop is trusted, i.e. when the caller
// itself is inside a trusted range.)
func newIPExtractor(raw []string) (echo.IPExtractor, string, error) {
	ranges, err := parseTrustedProxyCIDRs(raw)
	if err != nil {
		return nil, "", err
	}
	if len(ranges) == 0 {
		return echo.ExtractIPDirect(), ipTrustDirectMode, nil
	}
	options := []echo.TrustOption{
		echo.TrustLoopback(false),
		echo.TrustPrivateNet(false),
		echo.TrustLinkLocal(false),
	}
	trusted := make([]string, 0, len(ranges))
	for _, r := range ranges {
		options = append(options, echo.TrustIPRange(r))
		trusted = append(trusted, r.String())
	}
	return echo.ExtractIPFromXFFHeader(options...),
		fmt.Sprintf("%s: X-Forwarded-For from %d trusted proxy CIDR(s): %s",
			trustedProxyEnvVar, len(ranges), strings.Join(trusted, ", ")),
		nil
}
