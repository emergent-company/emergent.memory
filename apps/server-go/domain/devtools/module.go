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
	// Only enable in development/debug mode
	if !cfg.Debug {
		log.Info("devtools endpoints disabled (not in debug mode)")
		return
	}

	// Coverage endpoint - serves HTML coverage report
	e.GET("/coverage", h.ServeCoverage)
	e.GET("/coverage/*", h.ServeCoverageFiles)

	// Docs endpoint - serves OpenAPI/Swagger UI
	e.GET("/docs", h.ServeDocsIndex)
	e.GET("/docs/", h.ServeDocsIndex)
	e.GET("/docs/*", h.ServeDocs)

	// OpenAPI spec endpoint
	e.GET("/openapi.json", h.ServeOpenAPISpec)

	log.Info("devtools endpoints registered",
		slog.String("coverage", "/coverage"),
		slog.String("docs", "/docs"),
		slog.String("openapi", "/openapi.json"),
	)
}
