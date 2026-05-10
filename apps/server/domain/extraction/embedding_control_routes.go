package extraction

import (
	"github.com/labstack/echo/v4"
)

// RegisterEmbeddingControlRoutes registers the embedding control endpoints.
// No auth required — these are internal operational endpoints.
func RegisterEmbeddingControlRoutes(e *echo.Echo, h *EmbeddingControlHandler) {
	e.GET("/api/embeddings/status", h.Status)
	e.GET("/api/embeddings/progress", h.Progress)
	e.POST("/api/embeddings/pause", h.Pause)
	e.POST("/api/embeddings/resume", h.Resume)
	e.PATCH("/api/embeddings/config", h.Config)
	e.DELETE("/api/embeddings/queue", h.ClearQueue)
	e.POST("/api/embeddings/reset-schedule", h.ResetSchedule)
	e.GET("/api/embeddings/diagnose", h.DiagnoseQueue)
}
