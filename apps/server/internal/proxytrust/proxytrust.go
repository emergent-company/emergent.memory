// Package proxytrust builds the echo client-IP extractor from explicit
// configuration.
//
// Both this API server and the web-UI gateway need the same trust model:
// X-Forwarded-For is honoured only when the request arrived from a proxy the
// operator named, and never from echo v4's permissive default — that default
// trusts loopback, private and link-local peers, which is the trust model
// behind GHSA-246p-cpwv-v3jq (rate-limiter bypass via spoofed X-Forwarded-For)
// and GHSA-99jh-6h7p-pp36 (attacker-supplied X-Real-IP forwarded by default).
// Unset configuration fails closed: the extractor only ever returns the socket
// peer address, so no forwarded header can move a rate-limit bucket.
//
// The server (apps/server) and the gateway (apps/web-ui/gateway) are separate
// Go modules, and the gateway image is built from apps/web-ui/gateway alone
// with GOWORK=off, so this file cannot be imported there. gateway/ip_trust.go
// mirrors it; keep the two in step (both carry the same contract tests).
package proxytrust

import (
	"fmt"
	"net"
	"strings"

	"github.com/labstack/echo/v4"
)

// EnvVar is the environment variable holding the trusted proxy CIDRs
// (comma-separated). Empty means no peer is trusted and forwarded headers are
// ignored.
const EnvVar = "TRUSTED_PROXY_CIDRS"

// DirectMode is the startup-log description of the fail-closed default.
const DirectMode = "direct: no forwarded header trusted; set " + EnvVar +
	" to the proxy CIDR(s) if a proxy fronts this service"

// Parse validates a TRUSTED_PROXY_CIDRS-style list. Entries are trimmed and
// empty ones dropped; a malformed entry is an error rather than being ignored,
// so a typo cannot silently change the trust model. Empty input yields nil.
func Parse(raw []string) ([]*net.IPNet, error) {
	ranges := make([]*net.IPNet, 0, len(raw))
	for _, entry := range raw {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		_, ipNet, err := net.ParseCIDR(entry)
		if err != nil {
			return nil, fmt.Errorf("%s: invalid CIDR %q: %w", EnvVar, entry, err)
		}
		ranges = append(ranges, ipNet)
	}
	return ranges, nil
}

// New returns the echo IP extractor for the configured trusted proxy CIDRs,
// plus a one-line description for the startup log.
//
// With no trusted CIDRs it returns echo.ExtractIPDirect(), which ignores every
// request header and reports the socket peer. With trusted CIDRs it returns
// echo.ExtractIPFromXFFHeader with all implicit trust disabled, so the resolved
// address is the right-most hop that is not a configured proxy — the real
// client as appended by the outermost trusted proxy. (echo falls back to the
// left-most XFF element only when every hop is trusted, i.e. when the caller
// itself is inside a trusted range.)
func New(raw []string) (echo.IPExtractor, string, error) {
	ranges, err := Parse(raw)
	if err != nil {
		return nil, "", err
	}
	if len(ranges) == 0 {
		return echo.ExtractIPDirect(), DirectMode, nil
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
			EnvVar, len(ranges), strings.Join(trusted, ", ")),
		nil
}
