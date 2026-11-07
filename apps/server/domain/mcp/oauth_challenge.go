package mcp

import (
	"errors"
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// oauthChallengeMiddleware decorates a 401 from the auth middleware on an MCP
// route with the challenge required by the MCP authorization spec / RFC 9728:
//
//	WWW-Authenticate: Bearer resource_metadata="https://host/.well-known/oauth-protected-resource/api/mcp"
//
// Clients that support OAuth use the challenge to find the metadata directly.
// Without it they fall back to probing well-known paths, and clients that skip
// the fallback (e.g. Google's Gemini connector) report the server as
// unreachable instead of starting the authorization flow.
//
// The middleware must be registered before RequireAuth so it wraps the auth
// failure: it sets the header on the response and re-returns the error, and
// the server's error handler writes the body afterwards.
func oauthChallengeMiddleware(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		err := next(c)
		if err == nil || !isUnauthorized(err) {
			return err
		}
		c.Response().Header().Set("WWW-Authenticate",
			`Bearer resource_metadata="`+protectedResourceMetadataURL(c)+`"`)
		return err
	}
}

// isUnauthorized reports whether err will be rendered as a 401. The auth
// middleware returns *apperror.Error while some paths return *echo.HTTPError.
func isUnauthorized(err error) bool {
	var httpErr *echo.HTTPError
	if errors.As(err, &httpErr) && httpErr.Code == http.StatusUnauthorized {
		return true
	}
	var appErr *apperror.Error
	return errors.As(err, &appErr) && appErr.HTTPStatus == http.StatusUnauthorized
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
