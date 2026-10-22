package mcpregistry

import (
	"net/http"

	"github.com/emergent-company/emergent.memory/internal/outbound"
)

// outboundHTTPClient returns the *http.Client used by every outbound MCP
// transport (SSE and streamable HTTP) so a caller-supplied server URL cannot
// make the server fetch internal infrastructure.
//
// The SSRF guard (DialContext + redirect policy) lives in internal/outbound;
// this wrapper layers the MCP-specific authStatusTransport on top so an
// authentication rejection (HTTP 401/403) surfaces as a typed ErrMCPAuthRejected
// instead of a generic connection failure.
func outboundHTTPClient(rawURL string) *http.Client {
	c := outbound.NewClient(rawURL, outbound.DefaultPolicy())
	c.Transport = authStatusTransport{base: c.Transport}
	return c
}

// outboundProbeClient returns the same SSRF-guarded client as
// outboundHTTPClient but WITHOUT authStatusTransport, so the response status and
// headers of an authentication challenge are observable.
//
// The OAuth discovery probe needs exactly that: a 401's WWW-Authenticate header
// is the only place the resource metadata URL is advertised, and
// authStatusTransport discards the response before any header can be read.
func outboundProbeClient(rawURL string) *http.Client {
	return outbound.NewProbeClient(rawURL, outbound.DefaultPolicy())
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
