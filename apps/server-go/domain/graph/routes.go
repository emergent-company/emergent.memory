package graph

import (
	"github.com/labstack/echo/v4"

	"github.com/emergent/emergent-core/pkg/auth"
)

// RegisterRoutes registers all graph routes.
func RegisterRoutes(e *echo.Echo, h *Handler, authMiddleware *auth.Middleware) {
	// All graph routes require authentication and project context
	g := e.Group("/api/graph")
	g.Use(authMiddleware.RequireAuth())

	// Object routes
	objects := g.Group("/objects")
	objects.GET("/search", h.ListObjects)
	objects.GET("/fts", h.FTSSearch)
	objects.POST("/vector-search", h.VectorSearch)
	objects.GET("/tags", h.GetTags)
	objects.POST("/bulk-update-status", h.BulkUpdateStatus)
	objects.GET("/:id", h.GetObject)
	objects.GET("/:id/similar", h.GetSimilarObjects) // New: similar objects
	objects.POST("", h.CreateObject)
	objects.PATCH("/:id", h.PatchObject)
	objects.DELETE("/:id", h.DeleteObject)
	objects.POST("/:id/restore", h.RestoreObject)
	objects.GET("/:id/history", h.GetObjectHistory)
	objects.GET("/:id/edges", h.GetObjectEdges)

	// Hybrid search route (top level under /graph)
	g.POST("/search", h.HybridSearch)

	// New search with neighbors route
	g.POST("/search-with-neighbors", h.SearchWithNeighbors)

	// New graph expansion/traversal routes
	g.POST("/expand", h.ExpandGraph)
	g.POST("/traverse", h.TraverseGraph)

	// Branch routes
	branches := g.Group("/branches")
	branches.POST("/:targetBranchId/merge", h.MergeBranch)

	// Analytics routes
	analytics := g.Group("/analytics")
	analytics.GET("/most-accessed", h.GetMostAccessed)
	analytics.GET("/unused", h.GetUnused)

	// Relationship routes
	relationships := g.Group("/relationships")
	relationships.GET("/search", h.ListRelationships)
	relationships.GET("/:id", h.GetRelationship)
	relationships.POST("", h.CreateRelationship)
	relationships.PATCH("/:id", h.PatchRelationship)
	relationships.DELETE("/:id", h.DeleteRelationship)
	relationships.POST("/:id/restore", h.RestoreRelationship)
	relationships.GET("/:id/history", h.GetRelationshipHistory)
}
