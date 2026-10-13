package mcpregistry

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
)

// TestCompleteMCPOAuth_ProviderError covers the callback's denial path: when the
// authorization server redirects the browser back with an `error` parameter
// (e.g. access_denied), the handler must land the user back on the UI with a
// "denied" indication rather than exchanging anything. This path never reaches
// the service, so no database or network is required.
func TestCompleteMCPOAuth_ProviderError(t *testing.T) {
	svc, err := NewServiceWithOAuth(nil, nil, nil, OAuthConfig{
		RedirectBaseURL: "https://api.test",
		LandingURL:      "https://ui.test/settings/mcp-servers",
	}, slog.Default())
	require.NoError(t, err)
	h := NewHandler(svc)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet,
		"/api/mcp-servers/oauth/callback?error=access_denied&state=whatever", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	require.NoError(t, h.CompleteMCPOAuth(c))
	require.Equal(t, http.StatusSeeOther, rec.Code)
	require.Equal(t, "https://ui.test/settings/mcp-servers?mcp_oauth=denied", rec.Header().Get("Location"))
}
