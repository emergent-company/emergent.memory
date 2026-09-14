package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"
)

// isDialablePublicIP reports whether ip is a globally-routable unicast address
// the gateway may dial on behalf of a user-supplied URL. Loopback, private
// (RFC1918 + IPv6 ULA), link-local, multicast, unspecified, and
// carrier-grade-NAT (100.64.0.0/10, which includes some cloud metadata
// endpoints) addresses are rejected to prevent SSRF against internal services.
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

// ssrfProtectedDialContext resolves the target host and refuses to connect when
// any resolved address is not publicly routable. It dials the validated address
// directly, avoiding a DNS-rebinding TOCTOU between check and connect.
func ssrfProtectedDialContext(ctx context.Context, network, addr string) (net.Conn, error) {
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
	var lastErr error
	for _, ipAddr := range ips {
		if !isDialablePublicIP(ipAddr.IP) {
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
// addresses, for outbound requests to user-supplied URLs.
func newSSRFSafeHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			DialContext:         ssrfProtectedDialContext,
			ForceAttemptHTTP2:   true,
			MaxIdleConns:        10,
			IdleConnTimeout:     30 * time.Second,
			TLSHandshakeTimeout: 10 * time.Second,
		},
	}
}
