package templatepacks

import (
	"github.com/labstack/echo/v4"

	"github.com/emergent/emergent-core/pkg/auth"
)

// RegisterRoutes registers template pack routes
func RegisterRoutes(e *echo.Echo, h *Handler, authMiddleware *auth.Middleware) {
	// All template pack endpoints require authentication
	g := e.Group("/api/template-packs")
	g.Use(authMiddleware.RequireAuth())

	// Global template pack CRUD (not project-scoped)
	g.POST("", h.CreatePack)
	g.GET("/:packId", h.GetPack)
	g.DELETE("/:packId", h.DeletePack)

	// Project-scoped template pack routes
	projects := g.Group("/projects/:projectId")
	projects.Use(authMiddleware.RequireProjectScope())

	// Get available packs for a project
	projects.GET("/available", h.GetAvailablePacks)

	// Get installed packs for a project
	projects.GET("/installed", h.GetInstalledPacks)

	// Get compiled types for a project
	projects.GET("/compiled-types", h.GetCompiledTypes)

	// Assign a pack to a project
	projects.POST("/assign", h.AssignPack)

	// Update an assignment (e.g., toggle active)
	projects.PATCH("/assignments/:assignmentId", h.UpdateAssignment)

	// Delete an assignment
	projects.DELETE("/assignments/:assignmentId", h.DeleteAssignment)
}
