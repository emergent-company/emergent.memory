package mcpregistry

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
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

// TestMCPOAuthError_ClientRequiredMapsToBadRequest covers Defect 1: a rejected
// dynamic client registration must surface as a 400 with an actionable message,
// never an opaque 500 `internal_error`.
func TestMCPOAuthError_ClientRequiredMapsToBadRequest(t *testing.T) {
	// A registration rejection wrapped the way StartMCPOAuth wraps registerClient.
	err := fmt.Errorf("registering an OAuth client with the authorization server: %w",
		&oauthRegistrationRejected{Status: http.StatusForbidden, Excerpt: "access_denied"})

	mapped := mcpOAuthError(err)
	status, body := apperror.ToHTTPError(mapped)
	require.Equal(t, http.StatusBadRequest, status)

	blob, err := json.Marshal(body)
	require.NoError(t, err)
	require.NotContains(t, string(blob), "internal_error", "response body must not be an opaque internal error")

	msg, _ := body["error"].(map[string]any)["message"].(string)
	require.Contains(t, msg, "403", "message must name the HTTP status")
	require.Contains(t, msg, "pre-registered", "message must name the remedy")
}

// TestMCPOAuthError_ClientRequiredRegistrationUnavailable covers the
// registration-not-offered branch (no HTTP status to report).
func TestMCPOAuthError_ClientRequiredRegistrationUnavailable(t *testing.T) {
	err := fmt.Errorf("%w: the authorization server %q requires a pre-registered OAuth client (dynamic client registration is not offered)", ErrOAuthClientRequired, "https://auth.test")

	mapped := mcpOAuthError(err)
	status, body := apperror.ToHTTPError(mapped)
	require.Equal(t, http.StatusBadRequest, status)

	blob, err := json.Marshal(body)
	require.NoError(t, err)
	require.NotContains(t, string(blob), "internal_error")
}
