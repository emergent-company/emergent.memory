package health

import (
	"github.com/labstack/echo/v4"
)

// RegisterRoutes registers health check routes
func RegisterRoutes(e *echo.Echo, h *Handler, m *MetricsHandler) {
	e.GET("/health", h.Health)
	e.GET("/healthz", h.Healthz)
	e.GET("/ready", h.Ready)
	e.GET("/debug", h.Debug)
	e.GET("/api/health", h.Health)

	e.GET("/api/metrics/jobs", m.JobMetrics)
	e.GET("/api/metrics/scheduler", m.SchedulerMetrics)
}
