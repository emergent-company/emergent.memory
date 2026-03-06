package docs

import (
	"log/slog"

	"github.com/labstack/echo/v4"
)

func RegisterRoutes(e *echo.Echo, h *Handler, log *slog.Logger) {
	g := e.Group("/api/docs")

	g.GET("", h.ListDocuments)
	g.GET("/categories", h.GetCategories)
	g.GET("/:slug", h.GetDocument)

	log.Info("registered documentation routes", slog.String("prefix", "/api/docs"))
}
