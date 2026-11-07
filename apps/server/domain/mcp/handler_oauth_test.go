package mcp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/oauthas"
)

func protectedResourceMetadata(t *testing.T, svc *Service, req *http.Request) map[string]any {
	t.Helper()
	h := &Handler{svc: svc}
	e := echo.New()
	e.GET(prmWellKnownPath, h.HandleOAuthProtectedResource)
	e.GET(prmWellKnownPath+"/*", h.HandleOAuthProtectedResource)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	return body
}

func TestHandleOAuthProtectedResourceAdvertisesAuthorizationServer(t *testing.T) {
	svc := &Service{oauthASEnabled: true, oauthASIssuer: "https://api.example.com"}
	req := httptest.NewRequest(http.MethodGet, "https://api.example.com/.well-known/oauth-protected-resource", nil)
	req.Host = "api.example.com"

	body := protectedResourceMetadata(t, svc, req)
	require.Equal(t, []any{"https://api.example.com"}, body["authorization_servers"])
	require.Equal(t, "https://api.example.com", body["resource"])
}

func TestHandleOAuthProtectedResourceDerivesIssuerFromOrigin(t *testing.T) {
	svc := &Service{oauthASEnabled: true}
	req := httptest.NewRequest(http.MethodGet, "http://mcp.example.com/.well-known/oauth-protected-resource", nil)
	req.Host = "mcp.example.com"
	req.Header.Set("X-Forwarded-Proto", "https")

	body := protectedResourceMetadata(t, svc, req)
	require.Equal(t, []any{"https://mcp.example.com"}, body["authorization_servers"])
}

// TestHandleOAuthProtectedResourcePathInserted pins RFC 9728 §3.1/§3.3: the
// metadata for a path-bearing resource lives at the path-inserted well-known URI
// and its "resource" must be that URL, not the bare origin. A strict MCP client
// (Google's Gemini connector among them) rejects the origin form.
func TestHandleOAuthProtectedResourcePathInserted(t *testing.T) {
	svc := &Service{oauthASEnabled: true, oauthASIssuer: "https://api.example.com"}
	req := httptest.NewRequest(http.MethodGet, "https://api.example.com"+prmWellKnownPath+"/api/mcp", nil)
	req.Host = "api.example.com"

	body := protectedResourceMetadata(t, svc, req)
	require.Equal(t, "https://api.example.com/api/mcp", body["resource"])
	require.Equal(t, []any{"https://api.example.com"}, body["authorization_servers"])
}

// A resource path that is not the unified endpoint (an SSE route) keeps its own
// identifier, and a trailing slash is preserved so the value stays identical to
// what the client addressed (RFC 9728 §3.3).
func TestHandleOAuthProtectedResourceExactPathIdentity(t *testing.T) {
	svc := &Service{oauthASEnabled: true}
	for _, tt := range []struct {
		requestPath string
		want        string
	}{
		{"/.well-known/oauth-protected-resource/api/mcp/sse/p1", "https://api.example.com/api/mcp/sse/p1"},
		{"/.well-known/oauth-protected-resource/api/mcp/", "https://api.example.com/api/mcp/"},
		{"/.well-known/oauth-protected-resource/", "https://api.example.com"},
	} {
		req := httptest.NewRequest(http.MethodGet, "https://api.example.com"+tt.requestPath, nil)
		req.Host = "api.example.com"
		body := protectedResourceMetadata(t, svc, req)
		require.Equal(t, tt.want, body["resource"], tt.requestPath)
	}
}

func TestHandleOAuthProtectedResourceAdvertisesScopes(t *testing.T) {
	svc := &Service{oauthASEnabled: true}
	req := httptest.NewRequest(http.MethodGet, "https://api.example.com/.well-known/oauth-protected-resource", nil)
	req.Host = "api.example.com"

	body := protectedResourceMetadata(t, svc, req)
	scopes, ok := body["scopes_supported"].([]any)
	require.True(t, ok)
	require.Len(t, scopes, len(oauthas.SupportedScopes))
	for i, scope := range oauthas.SupportedScopes {
		require.Equal(t, scope, scopes[i])
	}
}

func TestHandleOAuthProtectedResourceDisabled(t *testing.T) {
	svc := &Service{}
	req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource", nil)
	req.Host = "api.example.com"

	body := protectedResourceMetadata(t, svc, req)
	require.Equal(t, []any{}, body["authorization_servers"])
}
