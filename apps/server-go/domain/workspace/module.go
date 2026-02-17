package workspace

import (
	"context"
	"log/slog"

	"github.com/labstack/echo/v4"
	"github.com/uptrace/bun"
	"go.uber.org/fx"

	"github.com/emergent/emergent-core/pkg/auth"
)

// Module provides workspace dependencies.
var Module = fx.Options(
	fx.Provide(newStoreFromDB),
	fx.Provide(newService),
	fx.Provide(newOrchestrator),
	fx.Provide(NewHandler),
	fx.Invoke(registerWorkspaceRoutes),
	fx.Invoke(registerProviders),
)

// registerWorkspaceRoutes registers workspace routes.
func registerWorkspaceRoutes(e *echo.Echo, h *Handler, authMiddleware *auth.Middleware, log *slog.Logger) {
	RegisterRoutes(e, h, authMiddleware, log)
}

// newStoreFromDB creates a workspace store with the bun DB.
func newStoreFromDB(db *bun.DB) *Store {
	return NewStore(db)
}

// newService creates a workspace service.
func newService(store *Store, log *slog.Logger) *Service {
	return NewService(store, log)
}

// newOrchestrator creates a workspace orchestrator.
func newOrchestrator(log *slog.Logger) *Orchestrator {
	return NewOrchestrator(log)
}

// registerProviders registers all available workspace providers with the orchestrator.
func registerProviders(lc fx.Lifecycle, orchestrator *Orchestrator, log *slog.Logger) {
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			// Register gVisor provider (always available — cross-platform fallback)
			gvisorProvider, err := NewGVisorProvider(log, nil)
			if err != nil {
				log.Warn("failed to create gVisor provider", "error", err)
			} else {
				orchestrator.RegisterProvider(ProviderGVisor, gvisorProvider)
			}

			// Start health monitoring
			orchestrator.StartHealthMonitoring(ctx)

			return nil
		},
		OnStop: func(_ context.Context) error {
			orchestrator.StopHealthMonitoring()
			return nil
		},
	})
}
