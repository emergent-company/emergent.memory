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
//   - /api/metrics/jobs — PROJECT-SCOPED. Gated on RequireAuth +
//     RequireProjectTokenScope + RequireProjectMember: the effective project is
//     resolved server-side (token binding or X-Project-ID header) and validated
//     against the caller's owning-org membership. A client-supplied ?project_id
//     is only a filter and can never widen access; the instance-wide aggregate
//     (no project context) is refused in the handler unless the caller holds
//     superadmin_full (issue #994 mechanism 3/4).
//   - /api/metrics/scheduler — INSTANCE-WIDE platform concern (deployment task
//     info). Gated on RequireAuth + RequireSuperadminFull, the same convention
//     as /debug and /api/diagnostics; a project member must not reach it.
func RegisterRoutes(e *echo.Echo, h *Handler, m *MetricsHandler, authMiddleware *auth.Middleware) {
	// Public probes.
	e.GET("/health", h.Health)
	e.GET("/healthz", h.Healthz)
	e.GET("/ready", h.Ready)
	e.GET("/api/health", h.Health)

	// Platform-tier internal diagnostics. Each surface is registered under its
	// own non-empty prefix so the group-level middleware's auto-registered
	// RouteNotFound catch-all is confined to that path. A group with an EMPTY
	// prefix (e.Group("")) would register a global "/*" catch-all and run the
	// superadmin guard — leaking 401/403 — onto every otherwise-unmatched
	// request across the whole API. See TestPlatformGateDoesNotLeakToUnrelatedPaths.
	debug := e.Group("/debug")
	debug.Use(authMiddleware.RequireAuth(), authMiddleware.RequireSuperadminFull())
	debug.GET("", h.Debug)

	diagnostics := e.Group("/api/diagnostics")
	diagnostics.Use(authMiddleware.RequireAuth(), authMiddleware.RequireSuperadminFull())
	diagnostics.GET("", h.Diagnose)

	// The scope-authority posture must not leak to anonymous callers (issue #812
	// Q5; retroactive #808 removed the oidc_all_grant signal from the anonymous
	// /health). Serve it on an authenticated surface only.
	authority := e.Group("/api/health")
	authority.Use(authMiddleware.RequireAuth())
	authority.GET("/scope-authority", h.ScopeAuthority)

	// Metrics endpoints carry per-route authority decisions so neither is left
	// behind RequireAuth-only (issue #994 mechanism 3/4):
	//   - /jobs       project-scoped (token binding + membership)
	//   - /scheduler  instance-wide (superadmin_full)
	metrics := e.Group("/api/metrics")
	metrics.Use(authMiddleware.RequireAuth())
	metrics.GET("/jobs", m.JobMetrics, authMiddleware.RequireProjectTokenScope(), authMiddleware.RequireProjectMember())
	metrics.GET("/scheduler", m.SchedulerMetrics, authMiddleware.RequireSuperadminFull())
}
