package chunks

import (
	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent/pkg/auth"
)

// RegisterRoutes registers the chunks routes
func RegisterRoutes(e *echo.Echo, handler *Handler, authMiddleware *auth.Middleware) {
	// All chunks routes require authentication and project ID
	chunks := e.Group("/api/chunks")
	chunks.Use(authMiddleware.RequireAuth())
	chunks.Use(authMiddleware.RequireProjectID())

	// List chunks (requires chunks:read scope)
	chunksRead := chunks.Group("")
	chunksRead.Use(authMiddleware.RequireScopes("chunks:read"))
	chunksRead.GET("", handler.List)

	// Write operations (requires chunks:write scope)
	chunksWrite := chunks.Group("")
	chunksWrite.Use(authMiddleware.RequireScopes("chunks:write"))
	chunksWrite.DELETE("/:id", handler.Delete)
	chunksWrite.DELETE("", handler.BulkDelete)
	chunksWrite.DELETE("/by-document/:documentId", handler.DeleteByDocument)
	chunksWrite.DELETE("/by-documents", handler.BulkDeleteByDocuments)
}
