package mcpregistry

import (
	"context"
	"os"
	"testing"
)

// TestDiscoverServerAuthRequirement_LiveFigma probes a real OAuth-protected MCP
// server (Figma) over the real SSRF-guarded outbound client, proving the
// discovery path works against production metadata rather than only against the
// test seam.
//
// Skipped unless MCPREGISTRY_LIVE_OAUTH_PROBE=1, so CI never depends on a
// third-party endpoint:
//
//	MCPREGISTRY_LIVE_OAUTH_PROBE=1 go test ./domain/mcpregistry/ -run LiveFigma -v
func TestDiscoverServerAuthRequirement_LiveFigma(t *testing.T) {
	if os.Getenv("MCPREGISTRY_LIVE_OAUTH_PROBE") != "1" {
		t.Skip("set MCPREGISTRY_LIVE_OAUTH_PROBE=1 to probe the live Figma MCP server")
	}

	server := &MCPServer{
		ID:   "live",
		Type: ServerTypeHTTP,
		URL:  strPtr("https://mcp.figma.com/mcp"),
	}

	req, err := discoverServerAuthRequirement(context.Background(), server)
	if err != nil {
		t.Fatalf("discoverServerAuthRequirement: %v", err)
	}
	t.Logf("authorization server: %s", req.AuthorizationServer)
	t.Logf("authorization endpoint: %s", req.AuthorizationEndpoint)
	t.Logf("token endpoint: %s", req.TokenEndpoint)
	t.Logf("registration endpoint: %s (supported=%v)", req.RegistrationEndpoint, req.RegistrationSupported)
	t.Logf("resource: %s", req.Resource)
	t.Logf("scopes: %v", req.Scopes)

	if req.AuthorizationEndpoint == "" || req.TokenEndpoint == "" {
		t.Fatal("authorization or token endpoint missing from live metadata")
	}
	if req.Resource == "" {
		t.Fatal("resource identifier missing: the authorize request could not carry RFC 8707 resource")
	}
}

// TestDiscoverServerAuthRequirement_LiveGitHub probes GitHub's remote MCP server
// (https://api.githubcopilot.com/mcp), which exercises the OpenID Connect
// discovery fallback: its authorization server (https://github.com/login/oauth)
// serves OIDC at the issuer path, not RFC 8414 metadata. Skipped unless
// MCPREGISTRY_LIVE_OAUTH_PROBE=1:
//
//	MCPREGISTRY_LIVE_OAUTH_PROBE=1 go test ./domain/mcpregistry/ -run LiveGitHub -v
func TestDiscoverServerAuthRequirement_LiveGitHub(t *testing.T) {
	if os.Getenv("MCPREGISTRY_LIVE_OAUTH_PROBE") != "1" {
		t.Skip("set MCPREGISTRY_LIVE_OAUTH_PROBE=1 to probe the live GitHub MCP server")
	}

	server := &MCPServer{
		ID:   "live-github",
		Type: ServerTypeHTTP,
		URL:  strPtr("https://api.githubcopilot.com/mcp/"),
	}

	req, err := discoverServerAuthRequirement(context.Background(), server)
	if err != nil {
		t.Fatalf("discoverServerAuthRequirement: %v", err)
	}
	t.Logf("authorization server: %s", req.AuthorizationServer)
	t.Logf("authorization endpoint: %s", req.AuthorizationEndpoint)
	t.Logf("token endpoint: %s", req.TokenEndpoint)
	t.Logf("registration endpoint: %s (supported=%v)", req.RegistrationEndpoint, req.RegistrationSupported)
	t.Logf("resource: %s", req.Resource)
	t.Logf("scopes: %v", req.Scopes)

	if req.AuthorizationEndpoint == "" || req.TokenEndpoint == "" {
		t.Fatal("authorization or token endpoint missing from live GitHub metadata (OIDC fallback did not resolve)")
	}
}
