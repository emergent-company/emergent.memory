package graph

import (
	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/pkg/auth"
	"github.com/emergent-company/emergent.memory/pkg/governor"
)

// bulkAdmit wraps a bulk handler with the resource governor's admission
// middleware. The governor is tier-neutral — it only sheds/limits load, never
// grants or removes authority — so it is applied as a wrapper handler rather
// than inline route middleware. internal/routeguard fails closed on inline
// middleware it cannot classify, and wrapping keeps each bulk route's authority
// tier derived from the group's auth middleware alone.
func bulkAdmit(gov *governor.Governor, next echo.HandlerFunc) echo.HandlerFunc {
	return gov.Middleware(governor.ClassBulk)(next)
}

// RegisterRoutes registers all graph routes.
func RegisterRoutes(e *echo.Echo, h *Handler, authMiddleware *auth.Middleware, gov *governor.Governor) {
	// All graph routes require authentication and project context. The shared
	// pair runs in canonical order: RequireProjectTokenScope binds an emt_* token
	// to its project, then RequireProjectMember asserts real org membership for
	// session/account-token callers (issue #909). These routes are header-scoped
	// (no :projectId path param), which is exactly the user.ProjectID fallback
	// RequireProjectMember exists for (#872).
	g := e.Group("/api/graph")
	g.Use(authMiddleware.RequireAuth())
	g.Use(authMiddleware.RequireProjectTokenScope())
	g.Use(authMiddleware.RequireProjectMember())

	// Bulk endpoints are gated by the resource governor's admission control:
	// under saturation these heavy requests are shed with HTTP 429 + Retry-After.
	// Object routes
	objects := g.Group("/objects")
	objects.GET("/search", h.ListObjects)
	objects.GET("/count", h.CountObjects)
	objects.GET("/fts", h.FTSSearch)
	objects.POST("/vector-search", h.VectorSearch)
	objects.GET("/tags", h.GetTags)
	// Deliberately ungated: bulk-update-status is a read-only status flip (no
	// heavy DB work), so it stays exempt from the resource governor's bulk
	// admission control.
	objects.POST("/bulk-update-status", h.BulkUpdateStatus)
	objects.POST("/bulk", bulkAdmit(gov, h.BulkCreateObjects))
	objects.POST("/bulk-update", bulkAdmit(gov, h.BulkUpdateObjects))
	objects.POST("/bulk-upsert", bulkAdmit(gov, h.BulkUpsertObjects))
	objects.POST("/bulk-action", bulkAdmit(gov, h.BulkAction))
	objects.POST("/validate", h.ValidateObject)
	objects.PUT("/upsert", h.UpsertObject)
	objects.GET("/:id", h.GetObject)
	objects.GET("/:id/contributions", h.GetObjectContributions)
	objects.GET("/:id/similar", h.GetSimilarObjects) // New: similar objects
	objects.POST("", h.CreateObject)
	objects.PATCH("/:id", h.PatchObject)
	objects.DELETE("/:id", h.DeleteObject)
	objects.POST("/:id/restore", h.RestoreObject)
	objects.POST("/:id/move", h.MoveObject)
	objects.GET("/:id/history", h.GetObjectHistory)
	objects.GET("/:id/edges", h.GetObjectEdges)

	// Hybrid search route (top level under /graph)
	g.POST("/search", h.HybridSearch)

	// New search with neighbors route
	g.POST("/search-with-neighbors", h.SearchWithNeighbors)

	// New graph expansion/traversal routes
	g.POST("/expand", h.ExpandGraph)
	g.POST("/traverse", h.TraverseGraph)

	// Atomic subgraph creation
	g.POST("/subgraph", h.CreateSubgraph)

	// Source changeset apply: a batch of upserts and withdrawals addressed by the
	// pushing source's own external ids and revisions (#2376, PR B).
	g.POST("/changesets", bulkAdmit(gov, h.ApplyChangeset))

	// Source outcome feedback: what became of each record a source pushed
	// (#2376, slice 5). A pull surface with a cursor; no webhooks in v1.
	g.GET("/sources/:id/outcomes", h.ListSourceOutcomes)

	// Reindex missing embeddings (recovery for ingest-without-schema scenarios)
	g.POST("/reindex-embeddings", bulkAdmit(gov, h.ReindexEmbeddings))

	// Branch routes
	branches := g.Group("/branches")
	branches.POST("/:targetBranchId/merge", h.MergeBranch)
	branches.GET("/:branchId/merge-readiness", h.MergeReadiness)
	branches.GET("/:branchId/compare", h.CompareBranches)
	branches.POST("/:id/fork", h.ForkBranch)

	// Analytics routes
	analytics := g.Group("/analytics")
	analytics.GET("/most-accessed", h.GetMostAccessed)
	analytics.GET("/unused", h.GetUnused)

	// Relationship routes
	relationships := g.Group("/relationships")
	relationships.GET("/search", h.ListRelationships)
	relationships.GET("/count", h.CountRelationships)
	relationships.POST("/bulk", bulkAdmit(gov, h.BulkCreateRelationships))
	relationships.POST("/bulk-upsert", bulkAdmit(gov, h.BulkUpsertRelationships))
	relationships.PUT("/upsert", h.UpsertRelationship)
	relationships.GET("/:id", h.GetRelationship)
	relationships.POST("", h.CreateRelationship)
	relationships.PATCH("/:id", h.PatchRelationship)
	relationships.DELETE("/:id", h.DeleteRelationship)
	relationships.POST("/:id/restore", h.RestoreRelationship)
	relationships.GET("/:id/history", h.GetRelationshipHistory)
}
