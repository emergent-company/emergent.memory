package superadmin

import (
	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// RegisterRoutes registers superadmin routes.
//
// Transport-level authorization (#1086): every route under /api/superadmin is
// gated at the route layer, so a newly added route cannot silently inherit only
// RequireAuth. The single exception is /api/superadmin/me, which is
// intentionally reachable by any authenticated user (it returns null for
// non-superadmins). The handler checks (requireSuperadmin for any role,
// requireSuperadminRole(RoleSuperadminFull) for mutations) remain as defence in
// depth and preserve the superadmin_readonly / superadmin_full split exactly.
func RegisterRoutes(e *echo.Echo, h *Handler, authMiddleware *auth.Middleware) {
	// All superadmin endpoints require authentication
	g := e.Group("/api/superadmin")
	g.Use(authMiddleware.RequireAuth())

	// Get current user's superadmin status (accessible to all authenticated users)
	g.GET("/me", h.GetMe)

	// Everything below this point requires an active superadmin grant at the
	// transport layer (superadmin_full or superadmin_readonly). Mutating routes
	// add RequireSuperadminFull inline, so the full-vs-readonly distinction is
	// preserved even before the handler re-checks the role.
	g.Use(authMiddleware.RequireSuperadmin())

	// Users management
	g.GET("/users", h.ListUsers)
	g.DELETE("/users/:id", h.DeleteUser, authMiddleware.RequireSuperadminFull())

	// Organizations management
	g.GET("/organizations", h.ListOrganizations)
	g.DELETE("/organizations/:id", h.DeleteOrganization, authMiddleware.RequireSuperadminFull())

	// Projects management
	g.GET("/projects", h.ListProjects)
	g.DELETE("/projects/:id", h.DeleteProject, authMiddleware.RequireSuperadminFull())
	g.GET("/projects/:id/members", h.ListProjectMembers)
	g.POST("/projects/:id/members", h.AddProjectMember, authMiddleware.RequireSuperadminFull())
	g.DELETE("/projects/:id/members/:userId", h.RemoveProjectMember, authMiddleware.RequireSuperadminFull())

	// Email jobs management
	g.GET("/email-jobs", h.ListEmailJobs)
	g.GET("/email-jobs/:id/preview-json", h.GetEmailJobPreview)

	// Embedding jobs management
	g.GET("/embedding-jobs", h.ListEmbeddingJobs)
	g.POST("/embedding-jobs/delete", h.DeleteEmbeddingJobs, authMiddleware.RequireSuperadminFull())
	g.POST("/embedding-jobs/cleanup-orphans", h.CleanupOrphanEmbeddingJobs, authMiddleware.RequireSuperadminFull())
	g.POST("/embedding-jobs/reset-dead-letter", h.ResetDeadLetterEmbeddingJobs, authMiddleware.RequireSuperadminFull())

	// Extraction jobs management
	g.GET("/extraction-jobs", h.ListExtractionJobs)
	g.POST("/extraction-jobs/delete", h.DeleteExtractionJobs, authMiddleware.RequireSuperadminFull())
	g.POST("/extraction-jobs/cancel", h.CancelExtractionJobs, authMiddleware.RequireSuperadminFull())

	// Document parsing jobs management
	g.GET("/document-parsing-jobs", h.ListDocumentParsingJobs)
	g.POST("/document-parsing-jobs/delete", h.DeleteDocumentParsingJobs, authMiddleware.RequireSuperadminFull())
	g.POST("/document-parsing-jobs/retry", h.RetryDocumentParsingJobs, authMiddleware.RequireSuperadminFull())

	// Service tokens (machine-to-machine access)
	g.POST("/service-tokens", h.CreateServiceToken, authMiddleware.RequireSuperadminFull())
}
