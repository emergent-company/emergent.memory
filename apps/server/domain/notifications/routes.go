package notifications

import (
	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/pkg/auth"
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

	// Mark a notification as read / unread
	g.PATCH("/:id/read", h.MarkRead)
	g.POST("/:id/unread", h.MarkUnread)

	// Dismiss a notification
	g.DELETE("/:id/dismiss", h.Dismiss)

	// Snooze / unsnooze
	g.POST("/:id/snooze", h.Snooze)
	g.POST("/:id/unsnooze", h.Unsnooze)

	// Clear / restore
	g.POST("/:id/clear", h.Clear)
	g.POST("/:id/restore", h.Restore)

	// Resolve an actionable notification
	g.POST("/:id/resolve", h.Resolve)

	// Mark all notifications as read (scope-aware)
	g.POST("/mark-all-read", h.MarkAllRead)

	// Notification preferences
	g.GET("/preferences", h.GetPreferences)
	g.PUT("/preferences", h.SavePreference)
}
