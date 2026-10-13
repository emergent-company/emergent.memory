package mcpregistry

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"time"
)

// outboundHTTPClient returns the *http.Client used by every outbound MCP
// transport (SSE and streamable HTTP) so a caller-supplied server URL cannot
// make the server fetch internal infrastructure.
//
// Two layers of defence sit on top of the pre-flight validateOutboundFetchURL
// check every fetch entry point runs:
//
//   - DialContext: the target is resolved and re-checked against the blocked
//     address ranges immediately before the socket is opened, so a DNS rebind
//     between validation and connect cannot reach internal space. It is only
//     installed when no egress proxy is configured for the target: behind a
//     proxy the dial goes to the proxy (often a private address) and the target
//     itself is covered by the pre-flight check instead.
//   - CheckRedirect: every redirect target is validated with the same guard, so
//     a public URL cannot bounce the server into internal space. Redirects are
//     also capped.
func outboundHTTPClient(rawURL string) *http.Client {
	tr, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		// Defensive: never panic on an unexpected default transport; fall back
		// to a plain client (pre-flight validation still applies).
		return &http.Client{
			CheckRedirect: safeRedirectPolicy,
		}
	}
	tr = tr.Clone()

	if req, err := http.NewRequest(http.MethodGet, rawURL, nil); err == nil {
		if proxy, perr := http.ProxyFromEnvironment(req); perr == nil && proxy == nil {
			tr.DialContext = guardedDialContext(tr.DialContext)
		}
	}

	return &http.Client{
		Transport:     authStatusTransport{base: tr},
		CheckRedirect: safeRedirectPolicy,
	}
}

// outboundProbeClient returns the same SSRF-guarded client as
// outboundHTTPClient but WITHOUT authStatusTransport, so the response status and
// headers of an authentication challenge are observable.
//
// The OAuth discovery probe needs exactly that: a 401's WWW-Authenticate header
// is the only place the resource metadata URL is advertised, and
// authStatusTransport discards the response before any header can be read.
func outboundProbeClient(rawURL string) *http.Client {
	c := outboundHTTPClient(rawURL)
	if t, ok := c.Transport.(authStatusTransport); ok {
		c.Transport = t.base
	}
	return c
}

// authStatusTransport wraps the outbound transport so an authentication
// rejection (HTTP 401/403) surfaces as a typed ErrMCPAuthRejected instead of a
// generic connection failure. The status is otherwise opaque to the MCP client
// library, which reports only "error calling tool"/"starting HTTP client".
//
// A 401/403 is terminal for a credential-bearing request, so the response body
// is closed and the request fails fast rather than letting the MCP handshake
// continue with a body the server will reject downstream.
type authStatusTransport struct {
	base http.RoundTripper
}

func (t authStatusTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.base.RoundTrip(req)
	if err != nil {
		return resp, err
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		_ = resp.Body.Close()
		return nil, &ErrMCPAuthRejected{Status: resp.StatusCode}
	}
	return resp, nil
}

// safeRedirectPolicy validates each redirect hop with the shared SSRF guard and
// caps the chain length.
func safeRedirectPolicy(req *http.Request, via []*http.Request) error {
	if len(via) >= 5 {
		return errors.New("stopped after 5 redirects")
	}
	if err := validateOutboundFetchURL(req.Context(), req.URL.String()); err != nil {
		return fmt.Errorf("redirect blocked: %w", err)
	}
	return nil
}

// guardedDialContext wraps a dialer so every connection target is resolved and
// checked against the blocked-address guard before the socket is opened.
//
// A hostname is resolved exactly once: if every returned address is public the
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
