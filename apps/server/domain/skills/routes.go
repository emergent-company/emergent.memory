package skills

import (
	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// RegisterRoutes registers skill routes on the Echo instance.
func RegisterRoutes(e *echo.Echo, h *Handler, authMiddleware *auth.Middleware) {
	// Global skill endpoints (read: all authenticated users; write: restricted to superadmin via route middleware)
	global := e.Group("/api/skills")
	global.Use(authMiddleware.RequireAuth())
	global.GET("", h.ListGlobalSkills)
	global.POST("", h.CreateGlobalSkill)
	global.GET("/:id", h.GetSkill)
	global.PATCH("/:id", h.UpdateSkill)
	global.DELETE("/:id", h.DeleteSkill)

	// Org-scoped skill endpoints
	orgs := e.Group("/api/orgs/:orgId/skills")
	orgs.Use(authMiddleware.RequireAuth())
	orgs.GET("", h.ListOrgSkills)
	orgs.POST("", h.CreateOrgSkill)
	orgs.PATCH("/:id", h.UpdateOrgSkill)
	orgs.DELETE("/:id", h.DeleteOrgSkill)

	// Project-scoped skill endpoints
	projects := e.Group("/api/projects/:projectId/skills")
	projects.Use(authMiddleware.RequireAuth())
	projects.Use(authMiddleware.RequireProjectScope())
	projects.GET("", h.ListProjectSkills)
	projects.POST("", h.CreateProjectSkill)
	projects.PATCH("/:id", h.UpdateProjectSkill)
	projects.DELETE("/:id", h.DeleteProjectSkill)
}
