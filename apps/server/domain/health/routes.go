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

	// The scope-authority posture must not leak to anonymous callers (issue #812
	// Q5; retroactive #808 removed the oidc_all_grant signal from the anonymous
	// /health). Serve it on an authenticated surface only.
	authority := e.Group("/api/health")
	authority.Use(authMiddleware.RequireAuth())
	authority.GET("/scope-authority", h.ScopeAuthority)

	// Metrics endpoints require authentication — project tokens see only their project's data.
	metrics := e.Group("/api/metrics")
	metrics.Use(authMiddleware.RequireAuth())
	metrics.GET("/jobs", m.JobMetrics)
	metrics.GET("/scheduler", m.SchedulerMetrics)
}
