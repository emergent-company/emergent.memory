package mcp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
)

func protectedResourceMetadata(t *testing.T, svc *Service, req *http.Request) map[string]any {
	t.Helper()
	h := &Handler{svc: svc}
	e := echo.New()
	e.GET("/.well-known/oauth-protected-resource", h.HandleOAuthProtectedResource)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

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

func TestHandleOAuthProtectedResourceDisabled(t *testing.T) {
	svc := &Service{}
	req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource", nil)
	req.Host = "api.example.com"

	body := protectedResourceMetadata(t, svc, req)
	require.Equal(t, []any{}, body["authorization_servers"])
}
