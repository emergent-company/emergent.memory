package notifications

import (
	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent/pkg/auth"
)

// RegisterRoutes registers notification routes
func RegisterRoutes(e *echo.Echo, h *Handler, authMiddleware *auth.Middleware) {
	// All notification endpoints require authentication
	g := e.Group("/api/notifications")
	g.Use(authMiddleware.RequireAuth())

	// Get notification stats (unread, dismissed, total)
	g.GET("/stats", h.GetStats)

	// Get notification counts by tab
	g.GET("/counts", h.GetCounts)

	// List notifications with filters
	g.GET("", h.List)

	// Mark a notification as read
	g.PATCH("/:id/read", h.MarkRead)

	// Dismiss a notification
	g.DELETE("/:id/dismiss", h.Dismiss)

	// Mark all notifications as read
	g.POST("/mark-all-read", h.MarkAllRead)
}
