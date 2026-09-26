package sandboximages

import (
	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// RegisterRoutes registers sandbox image admin API routes.
//
// Authorization (issue #968, the #959/#964 class): sandbox images are
// project-scoped (kb.sandbox_images.project_id UUID NOT NULL — migration 00027
// creating kb.workspace_images, renamed to kb.sandbox_images by 00059), but the
// group previously gated every route on a bare `admin` API-token scope. That
// scope is mintable by any project member (#948/#949) and — because
// RequireAPITokenScopes no-ops for OAuth sessions — admitted any authenticated
// session with no ownership check. The group is header-scoped (the addressed
// project is the X-Project-ID header, normalised onto user.ProjectID), so the
// canonical RequireProjectTokenScope → RequireProjectMember pair is the
// authority; the scope gate is dropped. The :id read/delete handlers scope the
// resource to the caller's project (see handler.go), so a member cannot read or
// delete another project's image by ID.
func RegisterRoutes(e *echo.Echo, h *Handler, authMiddleware *auth.Middleware) {
	admin := e.Group("/api/admin/sandbox-images")
	admin.Use(authMiddleware.RequireAuth())
	admin.Use(authMiddleware.RequireProjectTokenScope())
	admin.Use(authMiddleware.RequireProjectMember())

	// Read operations
	admin.GET("", h.List)
	admin.GET("/:id", h.Get)

	// Write operations
	admin.POST("", h.Create)
	admin.DELETE("/:id", h.Delete)
}
