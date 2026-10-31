package oauthas

import (
	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// RegisterRoutes registers the OAuth 2.1 authorization-server routes.
//
// The metadata, registration, token and revocation endpoints are public by
// design: they are registered on the root Echo before/independent of any
// auth-gated group, and enforce their own guards (registration secret, PKCE,
// client validation). The authorize surfaces require a real gateway session
// bearer and are additionally protected by project-membership checks.
func RegisterRoutes(e *echo.Echo, h *Handler, authMiddleware *auth.Middleware) {
	if h.cfg == nil || !h.cfg.OAuthAS.Enabled {
		return
	}

	// Public endpoints (RFC 8414 / RFC 7591 / RFC 6749 / RFC 7009).
	e.GET("/.well-known/oauth-authorization-server", h.Metadata)
	e.POST("/api/oauth/register", h.Register)
	e.POST("/api/oauth/token", h.Token)
	e.POST("/api/oauth/revoke", h.Revoke)

	// Authenticated consent + grant management (gateway session bearer).
	e.GET("/api/oauth/authorize/context", h.AuthorizeContext, authMiddleware.RequireAuth())
	e.POST("/api/oauth/authorize", h.Authorize, authMiddleware.RequireAuth())
	e.GET("/api/oauth/grants", h.ListGrants, authMiddleware.RequireAuth())
	e.DELETE("/api/oauth/grants/:id", h.RevokeGrant, authMiddleware.RequireAuth())
}
