package documents

import (
	"github.com/labstack/echo/v4"

	"github.com/emergent/emergent-core/pkg/auth"
)

// RegisterRoutes registers document routes with the Echo router
func RegisterRoutes(e *echo.Echo, h *Handler, uploadHandler *UploadHandler, authMiddleware *auth.Middleware) {
	sourceTypesGroup := e.Group("/api/documents")
	sourceTypesGroup.Use(authMiddleware.RequireAuth())
	sourceTypesGroup.GET("/source-types", h.GetSourceTypes)

	g := e.Group("/api/documents")
	g.Use(authMiddleware.RequireAuth())
	g.Use(authMiddleware.RequireProjectID())

	readGroup := g.Group("")
	readGroup.Use(authMiddleware.RequireScopes("documents:read"))
	readGroup.GET("", h.List)
	readGroup.GET("/:id", h.GetByID)
	readGroup.GET("/:id/content", h.GetContent)
	readGroup.GET("/:id/download", h.Download)

	writeGroup := g.Group("")
	writeGroup.Use(authMiddleware.RequireScopes("documents:write"))
	writeGroup.POST("", h.Create)
	writeGroup.POST("/upload", h.Upload)
	writeGroup.POST("/upload/batch", uploadHandler.UploadBatch)

	deleteGroup := g.Group("")
	deleteGroup.Use(authMiddleware.RequireScopes("documents:delete"))
	deleteGroup.DELETE("", h.BulkDelete)
	deleteGroup.DELETE("/:id", h.Delete)
	deleteGroup.GET("/:id/deletion-impact", h.GetDeletionImpact)
	deleteGroup.POST("/deletion-impact", h.BulkDeletionImpact)

	legacyUpload := e.Group("/api/document-parsing-jobs")
	legacyUpload.Use(authMiddleware.RequireAuth())
	legacyUpload.Use(authMiddleware.RequireProjectID())
	legacyUpload.Use(authMiddleware.RequireScopes("documents:write"))
	legacyUpload.POST("/upload", uploadHandler.Upload)
}
