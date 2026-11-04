package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"
)

// isDialablePublicIP reports whether ip is a globally-routable unicast address
// the gateway may dial on behalf of a user-supplied URL. Loopback, private
// (RFC1918 + IPv6 ULA), link-local, multicast, unspecified, and
// carrier-grade-NAT (100.64.0.0/10, which includes some cloud metadata
// endpoints) addresses are rejected to prevent SSRF against internal services.
//
// A host or CIDR explicitly named in PROVIDER_PROBE_ALLOW (see dialAllowlist)
// is exempted for the provider probe only.
func isDialablePublicIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsInterfaceLocalMulticast() ||
		ip.IsMulticast() || ip.IsUnspecified() {
		return false
	}
	// 100.64.0.0/10 (CGNAT).
	if v4 := ip.To4(); v4 != nil && v4[0] == 100 && v4[1] >= 64 && v4[1] <= 127 {
		return false
	}
	return ip.IsGlobalUnicast()
}

// dialAllowlist is the operator-configured set of hostnames and CIDRs the
// provider probe may dial even when they resolve to non-public addresses
// (self-hosted LLM gateways such as litellm/ollama). A nil allowlist allows
// nothing beyond the default public-only policy.
type dialAllowlist struct {
	hosts    map[string]struct{}
	prefixes []netip.Prefix
}

// newDialAllowlist parses PROVIDER_PROBE_ALLOW entries (comma-separated
// hostnames and/or CIDRs). Hostnames are matched case-insensitively; entries
// containing "/" must parse as a CIDR. Invalid entries are logged and ignored,
// so one typo never disables the rest. An empty/invalid-only input returns nil
// (strict public-only policy).
func newDialAllowlist(entries []string) *dialAllowlist {
	var a *dialAllowlist
	for _, raw := range entries {
		e := strings.TrimSpace(raw)
		if e == "" {
			continue
		}
		if strings.Contains(e, "/") {
			p, err := netip.ParsePrefix(e)
			if err != nil {
				log.Printf("provider probe allowlist: ignoring invalid CIDR %q: %v", e, err)
				continue
			}
			if a == nil {
				a = &dialAllowlist{}
			}
			a.prefixes = append(a.prefixes, p.Masked())
			continue
		}
		if strings.ContainsAny(e, " \t@") {
			log.Printf("provider probe allowlist: ignoring invalid host %q", e)
			continue
		}
		if a == nil {
			a = &dialAllowlist{}
		}
		if a.hosts == nil {
			a.hosts = make(map[string]struct{})
		}
		a.hosts[strings.ToLower(e)] = struct{}{}
	}
	return a
}

// permitsHost reports whether host is explicitly allowlisted (case-insensitive).
func (a *dialAllowlist) permitsHost(host string) bool {
	if a == nil || len(a.hosts) == 0 {
		return false
	}
	_, ok := a.hosts[strings.ToLower(host)]
	return ok
}

// permitsIP reports whether ip falls inside an allowlisted CIDR.
func (a *dialAllowlist) permitsIP(ip net.IP) bool {
	if a == nil || len(a.prefixes) == 0 {
		return false
	}
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	addr = addr.Unmap()
	for _, p := range a.prefixes {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}

// ssrfProtectedDialContext resolves the target host and refuses to connect when
// any resolved address is not publicly routable, unless the host or address is
// named in the operator allowlist. It dials the validated address directly,
// avoiding a DNS-rebinding TOCTOU between check and connect.
func ssrfProtectedDialContext(ctx context.Context, network, addr string, allow *dialAllowlist) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("no addresses for %q", host)
	}
	dialer := &net.Dialer{}
	hostAllowed := allow.permitsHost(host)
	var lastErr error
	for _, ipAddr := range ips {
		if !hostAllowed && !isDialablePublicIP(ipAddr.IP) && !allow.permitsIP(ipAddr.IP) {
			lastErr = fmt.Errorf("refusing to connect to non-public address %s", ipAddr.IP)
			continue
		}
		conn, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(ipAddr.IP.String(), port))
		if dialErr == nil {
			return conn, nil
		}
		lastErr = dialErr
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no dialable address for %q", host)
	}
	return nil, lastErr
}

// newSSRFSafeHTTPClient returns a client that blocks connections to non-public
// addresses, for outbound requests to user-supplied URLs. allow names hosts and
// CIDRs the operator has exempted (nil = public-only).
func newSSRFSafeHTTPClient(timeout time.Duration, allow *dialAllowlist) *http.Client {
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				return ssrfProtectedDialContext(ctx, network, addr, allow)
			},
			ForceAttemptHTTP2:   true,
			MaxIdleConns:        10,
			IdleConnTimeout:     30 * time.Second,
			TLSHandshakeTimeout: 10 * time.Second,
		},
	}
}
