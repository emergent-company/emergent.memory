package extraction

import (
	"github.com/labstack/echo/v4"
)

// RegisterEmbeddingControlRoutes registers the embedding control endpoints.
// No auth required — these are internal operational endpoints.
func RegisterEmbeddingControlRoutes(e *echo.Echo, h *EmbeddingControlHandler) {
	e.GET("/api/embeddings/status", h.Status)
	e.POST("/api/embeddings/pause", h.Pause)
	e.POST("/api/embeddings/resume", h.Resume)
}
