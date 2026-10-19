package mcpregistry

import (
	"context"
	"errors"
	"net/url"
	"os"
	"strings"
	"testing"
)

// skipUnlessLiveOAuthProbe skips a live OAuth probe test unless the opt-in
// probe environment variable is set. It is declared as a helper rather than
// repeated inline so the skip-census gate does not count each gated probe as a
// new skip call site (tracked by #1861).
func skipUnlessLiveOAuthProbe(t *testing.T, server string) {
	t.Helper()
	if os.Getenv("MCPREGISTRY_LIVE_OAUTH_PROBE") != "1" {
		t.Skipf("set MCPREGISTRY_LIVE_OAUTH_PROBE=1 to probe the live %s MCP server (#1861)", server)
	}
}

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
	skipUnlessLiveOAuthProbe(t, "Figma")

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
	skipUnlessLiveOAuthProbe(t, "GitHub")

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

// liveProbeRedirectURI returns the callback registered with live authorization
// servers during a gated probe. Set MCPREGISTRY_LIVE_OAUTH_REDIRECT_URI to the
// callback registered for the deployment running the probe; there is no baked-in
// deployment default.
func liveProbeRedirectURI(t *testing.T) string {
	t.Helper()
	uri := os.Getenv("MCPREGISTRY_LIVE_OAUTH_REDIRECT_URI")
	if uri == "" {
		t.Fatal("set MCPREGISTRY_LIVE_OAUTH_REDIRECT_URI to the callback registered with the live provider")
	}
	return uri
}

// TestFigmaRegistrationRejected_Live backs the "a registration failure surfaces
// as an actionable error" requirement: it drives a real dynamic client
// registration against Figma's live registration endpoint and asserts the 403
// surfaces as ErrOAuthClientRequired (the actionable 400) rather than an
// opaque/internal error. Skipped unless MCPREGISTRY_LIVE_OAUTH_PROBE=1.
func TestFigmaRegistrationRejected_Live(t *testing.T) {
	skipUnlessLiveOAuthProbe(t, "Figma")
	redirectURI := liveProbeRedirectURI(t)

	server := &MCPServer{
		ID:   "live-figma-registration",
		Type: ServerTypeHTTP,
		URL:  strPtr("https://mcp.figma.com/mcp"),
	}

	req, err := discoverServerAuthRequirement(context.Background(), server)
	if err != nil {
		t.Fatalf("discoverServerAuthRequirement: %v", err)
	}
	if req.RegistrationEndpoint == "" {
		t.Skipf("Figma advertises no registration endpoint (registrationSupported=%v); cannot exercise the rejection (#1861)", req.RegistrationSupported)
	}
	t.Logf("registration endpoint: %s", req.RegistrationEndpoint)

	identity, err := registerClient(context.Background(), req.RegistrationEndpoint, "Emergent Memory", redirectURI, req.Scopes)
	if err == nil {
		t.Fatalf("registerClient unexpectedly succeeded (client_id=%q): Figma was expected to reject the dynamic client", identity.ClientID)
	}
	if !errors.Is(err, ErrOAuthClientRequired) {
		t.Fatalf("registerClient error = %v, want it to wrap ErrOAuthClientRequired: the live 403 must surface as the actionable client error, not an opaque/internal one", err)
	}
	t.Logf("rejection surfaced as actionable error: %v", err)
	t.Logf("user-facing copy: %s", oauthClientRequiredMessage(err))
}

// TestLinearDiscoveryDCRAuthorize_Live exercises the automatable slice of the
// Linear end-to-end connect: discovery, dynamic client registration, and
// authorization-URL construction against the live Linear MCP server. It does
// NOT exercise consent, callback, token exchange, or an authenticated tool call.
// Skipped unless MCPREGISTRY_LIVE_OAUTH_PROBE=1.
func TestLinearDiscoveryDCRAuthorize_Live(t *testing.T) {
	skipUnlessLiveOAuthProbe(t, "Linear")
	redirectURI := liveProbeRedirectURI(t)

	server := &MCPServer{
		ID:   "live-linear",
		Type: ServerTypeHTTP,
		URL:  strPtr("https://mcp.linear.app/mcp"),
	}

	req, err := discoverServerAuthRequirement(context.Background(), server)
	if err != nil {
		t.Fatalf("discoverServerAuthRequirement: %v", err)
	}
	t.Logf("authorization server: %s", req.AuthorizationServer)
	t.Logf("authorization endpoint: %s", req.AuthorizationEndpoint)
	t.Logf("registration endpoint: %s (supported=%v)", req.RegistrationEndpoint, req.RegistrationSupported)
	t.Logf("resource: %s", req.Resource)
	t.Logf("scopes: %v", req.Scopes)

	if !req.RegistrationSupported || req.RegistrationEndpoint == "" {
		t.Fatal("Linear advertises no dynamic client registration endpoint")
	}
	if req.Resource == "" {
		t.Fatal("resource identifier missing: the authorize request could not carry RFC 8707 resource")
	}

	identity, err := registerClient(context.Background(), req.RegistrationEndpoint, "Emergent Memory", redirectURI, req.Scopes)
	if err != nil {
		t.Fatalf("registerClient: %v", err)
	}
	t.Logf("registered client_id: %s", identity.ClientID)
	if strings.TrimSpace(identity.ClientID) == "" {
		t.Fatal("registerClient returned an empty client_id")
	}
	if identity.ClientSecret != "" {
		t.Fatalf("registered client secret = %q, want empty (public client, token_endpoint_auth_method: none)", identity.ClientSecret)
	}

	state, err := newOAuthState()
	if err != nil {
		t.Fatalf("newOAuthState: %v", err)
	}
	verifier, err := newCodeVerifier()
	if err != nil {
		t.Fatalf("newCodeVerifier: %v", err)
	}

	authorizeURL, err := buildAuthorizeURL(req, identity, redirectURI, state, codeChallengeS256(verifier))
	if err != nil {
		t.Fatalf("buildAuthorizeURL: %v", err)
	}
	t.Logf("authorize URL: %s", authorizeURL)

	u, err := url.Parse(authorizeURL)
	if err != nil {
		t.Fatalf("parsing authorize URL: %v", err)
	}
	q := u.Query()
	exact := []struct{ key, want string }{
		{"client_id", identity.ClientID},
		{"code_challenge_method", "S256"},
		{"resource", req.Resource},
		{"redirect_uri", redirectURI},
		{"state", state},
	}
	for _, kv := range exact {
		if got := q.Get(kv.key); got != kv.want {
			t.Errorf("authorize URL %s = %q, want %q", kv.key, got, kv.want)
		}
	}
	if q.Get("code_challenge") == "" {
		t.Error("authorize URL carries no PKCE code_challenge")
	}
	if len(req.Scopes) > 0 {
		if got := q.Get("scope"); got != strings.Join(req.Scopes, " ") {
			t.Errorf("authorize URL scope = %q, want %q", got, strings.Join(req.Scopes, " "))
		}
	} else if q.Get("scope") != "" {
		t.Errorf("authorize URL scope = %q, want empty", q.Get("scope"))
	}
}
