package blueprints

import (
	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// RegisterRoutes registers blueprint routes
func RegisterRoutes(e *echo.Echo, h *Handler, authMiddleware *auth.Middleware) {
	// All blueprint endpoints require authentication
	g := e.Group("/api/blueprints")
	g.Use(authMiddleware.RequireAuth())
	// Blueprint CRUD/apply scope is the caller's project (X-Project-ID header,
	// normalised onto user.ProjectID by RequireAuth). Enforce token binding and
	// session org-membership against that project (issue #868, the #864 class).
	g.Use(authMiddleware.RequireProjectTokenScope())
	g.Use(authMiddleware.RequireProjectMember())

	// Static sub-paths must be registered before /:id so they are not swallowed.
	g.GET("/name/:name/versions", h.ListVersionsByName)
	g.GET("/applied", h.ListAppliedBlueprints, authMiddleware.RequireProjectID())

	g.POST("", h.CreateBlueprint)
	g.GET("", h.ListBlueprints)
	g.GET("/:id", h.GetBlueprint)
	g.PUT("/:id", h.UpdateBlueprint)
	g.POST("/:id/publish", h.PublishBlueprint)
	g.POST("/:id/deprecate", h.DeprecateBlueprint)
	g.POST("/:id/versions", h.NewVersion)
	// Apply is project-scoped: RequireProjectID enforces the X-Project-ID
	// header which populates user.ProjectID (the target project).
	g.POST("/:id/apply", h.ApplyBlueprint, authMiddleware.RequireProjectID())
	g.POST("/:id/unapply", h.UnapplyBlueprint, authMiddleware.RequireProjectID())
	g.DELETE("/:id", h.DeleteBlueprint)
}
