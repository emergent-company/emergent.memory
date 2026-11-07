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

const wantChallenge = `Bearer resource_metadata="https://api.example.com/.well-known/oauth-protected-resource/api/mcp"`

// The auth middleware writes the 401 itself (authError -> c.JSON) and returns
// nil. This is the shape production takes, and the reason the challenge is set
// from Response.Before: a header set after the handler returns is already
// flushed and silently dropped.
func TestOAuthChallengeMiddlewareCoversSelfWrittenUnauthorized(t *testing.T) {
	e := challengeServer(func(c echo.Context) error {
		return c.JSON(http.StatusUnauthorized, map[string]any{
			"error": map[string]any{"code": "missing_token", "message": "Missing authorization token"},
		})
	}, "/api/mcp")

	req := httptest.NewRequest(http.MethodGet, "https://api.example.com/api/mcp", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.Equal(t, wantChallenge, rec.Header().Get("WWW-Authenticate"))
}

// A 401 returned as an error (server error handler renders it) must carry the
// challenge too.
func TestOAuthChallengeMiddlewareCoversReturnedError(t *testing.T) {
	e := challengeServer(func(c echo.Context) error {
		return apperror.ErrMissingToken
	}, "/api/mcp")

	req := httptest.NewRequest(http.MethodGet, "https://api.example.com/api/mcp", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.Equal(t, wantChallenge, rec.Header().Get("WWW-Authenticate"))
}

// The middleware is registered before RequireAuth on the /api/mcp group; this
// pins that ordering contract with an auth middleware that behaves exactly like
// the real one (writes the 401, returns nil).
func TestOAuthChallengeMiddlewareWrapsGroupAuth(t *testing.T) {
	e := echo.New()
	e.HTTPErrorHandler = apperror.HTTPErrorHandler(slog.New(slog.NewTextHandler(io.Discard, nil)))
	g := e.Group("/api/mcp")
	g.Use(oauthChallengeMiddleware)
	g.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			return c.JSON(http.StatusUnauthorized, map[string]any{"error": "missing_token"})
		}
	})
	g.GET("", func(c echo.Context) error { return c.NoContent(http.StatusOK) })

	req := httptest.NewRequest(http.MethodGet, "https://api.example.com/api/mcp", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.Equal(t, wantChallenge, rec.Header().Get("WWW-Authenticate"))
}

// The challenge names the metadata URL of the resource the request addressed,
// so the metadata handler answers with a matching "resource" value.
func TestOAuthChallengeMiddlewareUsesAddressedPath(t *testing.T) {
	e := challengeServer(func(c echo.Context) error {
		return c.JSON(http.StatusUnauthorized, map[string]any{"error": "missing_token"})
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

func TestOAuthChallengeMiddlewareIgnoresNonUnauthorized(t *testing.T) {
	e := challengeServer(func(c echo.Context) error {
		return c.JSON(http.StatusForbidden, map[string]any{"error": "forbidden"})
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
