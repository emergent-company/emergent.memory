package sandboximages

import (
	"context"
	"log/slog"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/uptrace/bun"
	"go.uber.org/fx"

	"github.com/emergent-company/emergent.memory/domain/sandbox"
	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// Module provides workspace image catalog dependencies.
var Module = fx.Options(
	fx.Provide(newStoreFromDB),
	fx.Provide(newServiceFromConfig),
	fx.Provide(NewHandler),
	fx.Invoke(registerRoutes),
	fx.Invoke(startAutoSeed),
	fx.Invoke(wireImageResolver),
)

// newStoreFromDB creates a sandbox images store with the bun DB.
func newStoreFromDB(db *bun.DB) *Store {
	return NewStore(db)
}

// newServiceFromConfig creates a sandbox images service with configuration.
func newServiceFromConfig(store *Store, log *slog.Logger, cfg *config.Config) *Service {
	return NewService(store, log, ServiceConfig{
		FirecrackerDataDir: cfg.Sandbox.FirecrackerDataDir,
	})
}

// registerRoutes registers workspace image routes if workspaces are enabled.
func registerRoutes(cfg *config.Config, e *echo.Echo, h *Handler, authMiddleware *auth.Middleware, log *slog.Logger) {
	if !cfg.Sandbox.IsEnabled() {
		log.Info("sandbox images disabled (ENABLE_AGENT_SANDBOXES=false), skipping route registration")
		return
	}
	RegisterRoutes(e, h, authMiddleware)
}

// startAutoSeed seeds built-in images for all projects on server startup.
func startAutoSeed(lc fx.Lifecycle, svc *Service, db *bun.DB, cfg *config.Config, log *slog.Logger) {
	if !cfg.Sandbox.IsEnabled() {
		return
	}

	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			// Delay to let standalone bootstrap and migrations run first
			go func() {
				time.Sleep(5 * time.Second)
				seedCtx := context.Background()

				// Query all project IDs
				var projectIDs []string
				err := db.NewSelect().
					TableExpr("kb.projects").
					Column("id").
					Scan(seedCtx, &projectIDs)
				if err != nil {
					log.Error("failed to query projects for image seeding", "error", err)
					return
				}

				if len(projectIDs) == 0 {
					log.Info("no projects found, skipping workspace image seeding")
					return
				}

				log.Info("seeding sandbox images for projects",
					"count", len(projectIDs),
				)

				for _, pid := range projectIDs {
					if err := svc.SeedBuiltIns(seedCtx, pid); err != nil {
						log.Error("failed to seed built-in images for project",
							"project_id", pid,
							"error", err,
						)
					}
				}
			}()
			return nil
		},
	})
}

// wireImageResolver injects the image catalog resolver into the auto-provisioner.
func wireImageResolver(svc *Service, provisioner *sandbox.AutoProvisioner, log *slog.Logger) {
	provisioner.SetImageResolver(svc.AsImageResolver())
	log.Info("sandbox image catalog resolver wired into auto-provisioner")
}
