package monitoring

import (
	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent/pkg/auth"
)

// RegisterRoutes registers monitoring routes with fx
func RegisterRoutes(e *echo.Echo, h *Handler, authMiddleware *auth.Middleware) {
	// All monitoring routes require authentication and extraction:read scope
	monitoring := e.Group("/api/monitoring")
	monitoring.Use(authMiddleware.RequireAuth())
	monitoring.Use(authMiddleware.RequireScopes("extraction:read"))

	// Extraction job endpoints
	monitoring.GET("/extraction-jobs", h.ListExtractionJobs)
	monitoring.GET("/extraction-jobs/:id", h.GetExtractionJobDetail)
	monitoring.GET("/extraction-jobs/:id/logs", h.GetExtractionJobLogs)
	monitoring.GET("/extraction-jobs/:id/llm-calls", h.GetExtractionJobLLMCalls)
}
