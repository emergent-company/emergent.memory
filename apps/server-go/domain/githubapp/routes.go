package githubapp

import (
	"github.com/labstack/echo/v4"

	"github.com/emergent/emergent-core/pkg/auth"
)

// RegisterRoutes registers GitHub App HTTP routes.
func RegisterRoutes(e *echo.Echo, h *Handler, authMiddleware *auth.Middleware) {
	// Settings routes (require auth + admin:write for all config changes)
	g := e.Group("/api/v1/settings/github")

	// Read operations — connection status
	readGroup := g.Group("")
	readGroup.Use(authMiddleware.RequireAuth())
	readGroup.Use(authMiddleware.RequireScopes("admin:read"))
	readGroup.GET("", h.GetStatus)

	// Write operations — connect, disconnect, CLI setup
	writeGroup := g.Group("")
	writeGroup.Use(authMiddleware.RequireAuth())
	writeGroup.Use(authMiddleware.RequireScopes("admin:write"))
	writeGroup.POST("/connect", h.Connect)
	writeGroup.GET("/callback", h.Callback)
	writeGroup.DELETE("", h.Disconnect)
	writeGroup.POST("/cli", h.CLISetup)

	// Webhook — no auth (GitHub sends these), but should verify signature
	e.POST("/api/v1/settings/github/webhook", h.Webhook)
}
