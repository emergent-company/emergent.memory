package mcpregistry

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestLandingURL_CarriesScopeProject verifies the callback landing carries the
// flow's project as an untrusted hint (the gateway validates it and ignores an
// inaccessible value), and omits it when the project is unknown.
func TestLandingURL_CarriesScopeProject(t *testing.T) {
	cfg := OAuthConfig{LandingURL: "https://ui.test/oauth/landing/mcp-servers"}

	got := cfg.landingURL("connected", "proj-1")
	require.Equal(t, "https://ui.test/oauth/landing/mcp-servers?mcp_oauth=connected&scope_project=proj-1", got)

	// Denied before the state is read → no project hint.
	got = cfg.landingURL("denied", "")
	require.Equal(t, "https://ui.test/oauth/landing/mcp-servers?mcp_oauth=denied", got)
}

// TestLandingURL_FallbackIsScopeAgnostic pins the unconfigured fallback to the
// gateway's global landing route: a scoped UI path cannot be derived here, and
// a bare /settings/mcp-servers 404s under the scoped-URL hard cut.
func TestLandingURL_FallbackIsScopeAgnostic(t *testing.T) {
	cfg := OAuthConfig{RedirectBaseURL: "https://api.test/"}
	got := cfg.landingURL("error", "proj-1")
	require.Equal(
		t,
		"https://api.test/oauth/landing/mcp-servers?mcp_oauth=error&scope_project=proj-1",
		got,
	)

	// No base URL at all → empty (the connect flow is refused before this).
	require.Empty(t, OAuthConfig{}.landingURL("connected", "proj-1"))
}
