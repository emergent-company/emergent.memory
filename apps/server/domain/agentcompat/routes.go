package agentcompat

import (
	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// RegisterRoutes mounts the OpenAI-compatible endpoints.
//
// Routes live under /v1/ (outside the /api/ namespace) to match the standard
// OpenAI base URL so existing SDKs work by only changing base_url.
//
// Authentication uses the same Bearer token mechanism as every other Memory
// route (emt_* API tokens or OAuth).  Project context is resolved from the
// token's bound project or the X-Project-ID header.
func RegisterRoutes(e *echo.Echo, h *Handler, authMiddleware *auth.Middleware) {
	v1 := e.Group("/v1")
	v1.Use(authMiddleware.RequireAuth())

	v1.POST("/chat/completions", h.ChatCompletion)
	v1.GET("/models", h.ListModels)
}
