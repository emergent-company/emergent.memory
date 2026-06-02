package extraction

import (
	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// RegisterEmbeddingControlRoutes registers the embedding control endpoints.
// All routes require authentication.
func RegisterEmbeddingControlRoutes(e *echo.Echo, h *EmbeddingControlHandler, authMiddleware *auth.Middleware) {
	g := e.Group("/api/embeddings")
	g.Use(authMiddleware.RequireAuth())

	g.GET("/status", h.Status)
	g.GET("/progress", h.Progress)
	g.POST("/pause", h.Pause)
	g.POST("/resume", h.Resume)
	g.PATCH("/config", h.Config)
	g.DELETE("/queue", h.ClearQueue)
	g.POST("/reset-schedule", h.ResetSchedule)
	g.GET("/diagnose", h.DiagnoseQueue)
}
