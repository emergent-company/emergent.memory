package schemas

import (
	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// RegisterRoutes registers schema routes
func RegisterRoutes(e *echo.Echo, h *Handler, authMiddleware *auth.Middleware) {
	// All schema endpoints require authentication
	g := e.Group("/api/schemas")
	g.Use(authMiddleware.RequireAuth())

	// Global template pack CRUD (not project-scoped)
	g.POST("", h.CreatePack)
	g.GET("/:packId", h.GetPack)
	g.PUT("/:packId", h.UpdatePack)
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

	// Get schema installation history (including soft-deleted)
	projects.GET("/history", h.GetSchemaHistory)

	// Validate graph objects (check for schema drift)
	projects.GET("/validate", h.ValidateObjects)

	// Migrate live graph data (type/property renames) — System B, unchanged
	projects.POST("/migrate", h.MigrateTypes)

	// Schema migration — System A (async, chain-aware)
	projects.POST("/migrate/preview", h.PreviewMigration)
	projects.POST("/migrate/execute", h.ExecuteMigration)
	projects.POST("/migrate/rollback", h.RollbackMigration)
	projects.POST("/migrate/commit", h.CommitMigrationArchive)
	projects.GET("/migration-jobs/:jobId", h.GetMigrationJobStatus)
}
