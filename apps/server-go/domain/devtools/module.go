package devtools

import (
	"log/slog"

	"github.com/labstack/echo/v4"
	"go.uber.org/fx"

	"github.com/emergent/emergent-core/internal/config"
)

// Module provides developer tools endpoints (coverage, docs)
var Module = fx.Module("devtools",
	fx.Provide(NewHandler),
	fx.Invoke(RegisterRoutes),
)

// RegisterRoutes registers all devtools routes
func RegisterRoutes(e *echo.Echo, h *Handler, cfg *config.Config, log *slog.Logger) {
	// API documentation endpoints - always available
	e.GET("/docs", h.ServeDocsIndex)
	e.GET("/docs/", h.ServeDocsIndex)
	e.GET("/docs/*", h.ServeDocs)
	e.GET("/openapi.json", h.ServeOpenAPISpec)

	log.Info("API documentation endpoints registered",
		slog.String("docs", "/docs"),
		slog.String("openapi", "/openapi.json"),
	)

	// Coverage endpoints - debug mode only
	if cfg.Debug {
		e.GET("/coverage", h.ServeCoverage)
		e.GET("/coverage/*", h.ServeCoverageFiles)

		log.Info("coverage endpoints registered (debug mode)",
			slog.String("coverage", "/coverage"),
		)
	}
}
