package health

import (
	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// RegisterRoutes registers health check routes
func RegisterRoutes(e *echo.Echo, h *Handler, m *MetricsHandler, authMiddleware *auth.Middleware) {
	e.GET("/health", h.Health)
	e.GET("/healthz", h.Healthz)
	e.GET("/ready", h.Ready)
	e.GET("/debug", h.Debug)
	e.GET("/api/health", h.Health)
	e.GET("/api/diagnostics", h.Diagnose)

	// Metrics endpoints require authentication — project tokens see only their project's data.
	metrics := e.Group("/api/metrics")
	metrics.Use(authMiddleware.RequireAuth())
	metrics.GET("/jobs", m.JobMetrics)
	metrics.GET("/scheduler", m.SchedulerMetrics)
}
