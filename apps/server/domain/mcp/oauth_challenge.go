package mcp

import (
	"net/http"

	"github.com/labstack/echo/v4"
)

// oauthChallengeMiddleware attaches the challenge required by the MCP
// authorization spec / RFC 9728 to a 401 on the /api/mcp group:
//
//	WWW-Authenticate: Bearer resource_metadata="https://host/.well-known/oauth-protected-resource/api/mcp"
//
// Clients that support OAuth use the challenge to find the metadata directly.
// Without it they fall back to probing well-known paths, and clients that skip
// the fallback (e.g. Google's Gemini connector) report the server as
// unreachable instead of starting the authorization flow.
//
// The header is set from Response.Before, not from the middleware's return
// value: the auth middleware writes the 401 itself (authError -> c.JSON) and
// returns nil, so by the time the chain unwinds the response is already
// flushed and a plain Header().Set would be a no-op. Before runs inside
// WriteHeader, after the status is recorded and before the headers are sent,
// which covers both shapes — a handler that writes the 401 and one that
// returns the error for the server's error handler.
//
// Register it before RequireAuth on the group so it also wraps the auth
// middleware.
func oauthChallengeMiddleware(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		res := c.Response()
		res.Before(func() {
			if res.Status == http.StatusUnauthorized {
				res.Header().Set("WWW-Authenticate",
					`Bearer resource_metadata="`+protectedResourceMetadataURL(c)+`"`)
			}
		})
		return next(c)
	}
}

// protectedResourceMetadataURL returns the RFC 9728 metadata URL for the
// resource addressed by the request. Only the request path is used, so a
// request to /api/mcp points at .../.well-known/oauth-protected-resource/api/mcp
// and the metadata handler answers with a matching "resource" value.
func protectedResourceMetadataURL(c echo.Context) string {
	path := c.Request().URL.Path
	if path == "" || path == "/" {
		path = "/api/mcp"
	}
	return requestOrigin(c) + prmWellKnownPath + path
}
