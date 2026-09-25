package health

import (
	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// RegisterRoutes registers health check routes.
//
// Route authority decisions:
//
//   - /health, /healthz, /ready, /api/health — DELIBERATELY PUBLIC. Kubernetes
//     liveness/readiness probes and load-balancer health checks must answer
//     without a credential. They return coarse service state only (no internals).
//   - /debug, /api/diagnostics — PLATFORM-TIER internal diagnostics (DB
//     connection target, pool state, pg_stat_activity). Gated on the shared
//     role-derived superadmin_full seam (RequireSuperadminFull), NOT a token
//     scope and NOT a bare admin scope (an org_admin-minted admin:all token must
//     not satisfy it) — the same convention as #947/#958.
//   - /api/health/scope-authority — authenticated only (RequireAuth); the
//     permissive scope-authority posture must not leak anonymously (#812).
//   - /api/metrics/{jobs,scheduler} — authenticated only (RequireAuth); project
//     tokens see only their own project's data.
func RegisterRoutes(e *echo.Echo, h *Handler, m *MetricsHandler, authMiddleware *auth.Middleware) {
	// Public probes.
	e.GET("/health", h.Health)
	e.GET("/healthz", h.Healthz)
	e.GET("/ready", h.Ready)
	e.GET("/api/health", h.Health)

	// Platform-tier internal diagnostics.
	platform := e.Group("")
	platform.Use(authMiddleware.RequireAuth())
	platform.Use(authMiddleware.RequireSuperadminFull())
	platform.GET("/debug", h.Debug)
	platform.GET("/api/diagnostics", h.Diagnose)

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
