package invites

import (
	"github.com/labstack/echo/v4"
	"go.uber.org/fx"

	"github.com/emergent-company/emergent/pkg/auth"
)

// Module provides the invites domain
var Module = fx.Module("invites",
	fx.Provide(NewService),
	fx.Provide(NewHandler),
	fx.Invoke(RegisterRoutes),
)

// RegisterRoutes registers the invites routes
func RegisterRoutes(e *echo.Echo, h *Handler, authMiddleware *auth.Middleware) {
	// Invites routes (require authentication)
	g := e.Group("/api/invites", authMiddleware.RequireAuth())

	// GET /api/invites/pending - list pending invitations for current user
	g.GET("/pending", h.ListPending)

	// POST /api/invites - create a new invitation
	g.POST("", h.Create)

	// POST /api/invites/accept - accept an invitation by token
	g.POST("/accept", h.Accept)

	// POST /api/invites/:id/decline - decline an invitation
	g.POST("/:id/decline", h.Decline)

	// DELETE /api/invites/:id - revoke/cancel an invitation
	g.DELETE("/:id", h.Delete)

	// Project-specific invite routes
	projects := e.Group("/api/projects", authMiddleware.RequireAuth())

	// GET /api/projects/:projectId/invites - list invites for a project
	projects.GET("/:projectId/invites", h.ListByProject)
}
