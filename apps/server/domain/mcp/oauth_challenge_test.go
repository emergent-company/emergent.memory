package mcp

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

func challengeServer(handler echo.HandlerFunc, route string) *echo.Echo {
	e := echo.New()
	e.HTTPErrorHandler = apperror.HTTPErrorHandler(slog.New(slog.NewTextHandler(io.Discard, nil)))
	e.GET(route, handler, oauthChallengeMiddleware)
	return e
}

func TestOAuthChallengeMiddlewareAddsResourceMetadata(t *testing.T) {
	e := challengeServer(func(c echo.Context) error {
		return apperror.ErrMissingToken
	}, "/api/mcp")

	req := httptest.NewRequest(http.MethodGet, "https://api.example.com/api/mcp", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.Equal(t,
		`Bearer resource_metadata="https://api.example.com/.well-known/oauth-protected-resource/api/mcp"`,
		rec.Header().Get("WWW-Authenticate"))
}

// The challenge names the metadata URL of the resource the request addressed,
// so the metadata handler answers with a matching "resource" value.
func TestOAuthChallengeMiddlewareUsesAddressedPath(t *testing.T) {
	e := challengeServer(func(c echo.Context) error {
		return apperror.ErrMissingToken
	}, "/api/mcp/sse/:projectId")

	req := httptest.NewRequest(http.MethodGet, "http://api.example.com/api/mcp/sse/p1", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.Equal(t,
		`Bearer resource_metadata="https://api.example.com/.well-known/oauth-protected-resource/api/mcp/sse/p1"`,
		rec.Header().Get("WWW-Authenticate"))
}

// The middleware is registered before RequireAuth on the /api/mcp group; this
// pins that ordering contract: when the auth middleware fails, the challenge is
// still set before the server's error handler writes the response.
func TestOAuthChallengeMiddlewareWrapsGroupAuth(t *testing.T) {
	e := echo.New()
	e.HTTPErrorHandler = apperror.HTTPErrorHandler(slog.New(slog.NewTextHandler(io.Discard, nil)))
	g := e.Group("/api/mcp")
	g.Use(oauthChallengeMiddleware)
	g.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error { return apperror.ErrMissingToken }
	})
	g.GET("", func(c echo.Context) error { return c.NoContent(http.StatusOK) })

	req := httptest.NewRequest(http.MethodGet, "https://api.example.com/api/mcp", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.Equal(t,
		`Bearer resource_metadata="https://api.example.com/.well-known/oauth-protected-resource/api/mcp"`,
		rec.Header().Get("WWW-Authenticate"))
}

func TestOAuthChallengeMiddlewareIgnoresNonUnauthorized(t *testing.T) {
	e := challengeServer(func(c echo.Context) error {
		return &apperror.Error{HTTPStatus: http.StatusForbidden, Code: "forbidden", Message: "no"}
	}, "/api/mcp")

	req := httptest.NewRequest(http.MethodGet, "https://api.example.com/api/mcp", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusForbidden, rec.Code)
	require.Empty(t, rec.Header().Get("WWW-Authenticate"))
}

func TestOAuthChallengeMiddlewarePassesThroughSuccess(t *testing.T) {
	e := challengeServer(func(c echo.Context) error {
		return c.NoContent(http.StatusNoContent)
	}, "/api/mcp")

	req := httptest.NewRequest(http.MethodGet, "https://api.example.com/api/mcp", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNoContent, rec.Code)
	require.Empty(t, rec.Header().Get("WWW-Authenticate"))
}
