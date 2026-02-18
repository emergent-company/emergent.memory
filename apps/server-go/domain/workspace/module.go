package workspace

import (
	"context"
	"log/slog"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/uptrace/bun"
	"go.uber.org/fx"

	"github.com/emergent-company/emergent/internal/config"
	"github.com/emergent-company/emergent/pkg/auth"
)

// Module provides workspace dependencies.
// All workspace functionality is gated behind the ENABLE_AGENT_WORKSPACES feature flag.
var Module = fx.Options(
	fx.Provide(newStoreFromDB),
	fx.Provide(newServiceFromConfig),
	fx.Provide(newOrchestrator),
	fx.Provide(newCleanupJob),
	fx.Provide(newSetupExecutor),
	fx.Provide(newCheckoutService),
	fx.Provide(newAutoProvisioner),
	fx.Provide(newWarmPool),
	fx.Provide(NewHandler),
	fx.Provide(newMCPHostingService),
	fx.Provide(newMCPHostingHandler),
	fx.Invoke(registerWorkspaceRoutes),
	fx.Invoke(registerProviders),
	fx.Invoke(startCleanupJob),
	fx.Invoke(startWarmPool),
	fx.Invoke(startMCPHosting),
)

// registerWorkspaceRoutes registers workspace routes only if the feature is enabled.
func registerWorkspaceRoutes(cfg *config.Config, e *echo.Echo, h *Handler, authMiddleware *auth.Middleware, log *slog.Logger) {
	if !cfg.Workspace.IsEnabled() {
		log.Info("agent workspaces disabled (ENABLE_AGENT_WORKSPACES=false), skipping route registration")
		return
	}
	RegisterRoutes(e, h, authMiddleware, log)
}

// newStoreFromDB creates a workspace store with the bun DB.
func newStoreFromDB(db *bun.DB) *Store {
	return NewStore(db)
}

// newServiceFromConfig creates a workspace service with configuration from env vars.
func newServiceFromConfig(store *Store, orchestrator *Orchestrator, log *slog.Logger, cfg *config.Config) *Service {
	svc := NewService(store, orchestrator, log)
	if cfg.Workspace.MaxConcurrent > 0 {
		svc.config.MaxConcurrent = cfg.Workspace.MaxConcurrent
	}
	if cfg.Workspace.DefaultTTLDays > 0 {
		svc.config.DefaultTTLDays = cfg.Workspace.DefaultTTLDays
	}
	if cfg.Workspace.DefaultProvider != "" {
		svc.config.DefaultProvider = ProviderType(cfg.Workspace.DefaultProvider)
	}
	if cfg.Workspace.DefaultCPU != "" {
		svc.config.DefaultCPU = cfg.Workspace.DefaultCPU
	}
	if cfg.Workspace.DefaultMemory != "" {
		svc.config.DefaultMemory = cfg.Workspace.DefaultMemory
	}
	if cfg.Workspace.DefaultDisk != "" {
		svc.config.DefaultDisk = cfg.Workspace.DefaultDisk
	}
	return svc
}

// newOrchestrator creates a workspace orchestrator.
func newOrchestrator(log *slog.Logger) *Orchestrator {
	return NewOrchestrator(log)
}

// newSetupExecutor creates a workspace setup command executor.
func newSetupExecutor(orchestrator *Orchestrator, log *slog.Logger) *SetupExecutor {
	return NewSetupExecutor(orchestrator, log)
}

// newCheckoutService creates a checkout service for git operations.
// For now, we pass nil as the credential provider, which means:
// - Public repositories work fine (unauthenticated clone)
// - Private repositories will fail (requires GitHub App integration)
// - Git identity falls back to "Emergent Agent <agent@emergent.local>"
func newCheckoutService(log *slog.Logger) *CheckoutService {
	return NewCheckoutService(nil, log)
}

// newAutoProvisioner creates the auto-provisioning service for agent workspaces.
func newAutoProvisioner(service *Service, orchestrator *Orchestrator, checkoutSvc *CheckoutService, setupExec *SetupExecutor, warmPool *WarmPool, log *slog.Logger) *AutoProvisioner {
	return NewAutoProvisioner(service, orchestrator, checkoutSvc, setupExec, warmPool, log)
}

// newCleanupJob creates a cleanup job with configuration from env vars.
func newCleanupJob(store *Store, orchestrator *Orchestrator, log *slog.Logger, cfg *config.Config) *CleanupJob {
	cleanupCfg := DefaultCleanupConfig()
	if cfg.Workspace.MaxConcurrent > 0 {
		cleanupCfg.MaxConcurrent = cfg.Workspace.MaxConcurrent
	}
	if cfg.Workspace.CleanupIntervalMin > 0 {
		cleanupCfg.Interval = time.Duration(cfg.Workspace.CleanupIntervalMin) * time.Minute
	}
	if cfg.Workspace.AlertThresholdPct > 0 && cfg.Workspace.AlertThresholdPct <= 100 {
		cleanupCfg.AlertThreshold = float64(cfg.Workspace.AlertThresholdPct) / 100.0
	}
	return NewCleanupJob(store, orchestrator, log, cleanupCfg)
}

// registerProviders registers all available workspace providers with the orchestrator.
func registerProviders(lc fx.Lifecycle, orchestrator *Orchestrator, cfg *config.Config, log *slog.Logger) {
	if !cfg.Workspace.IsEnabled() {
		return
	}

	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			// Register gVisor provider (always available — cross-platform fallback)
			gvisorCfg := &GVisorProviderConfig{
				NetworkName:  cfg.Workspace.NetworkName,
				DefaultImage: cfg.Workspace.DefaultImage,
			}
			gvisorProvider, err := NewGVisorProvider(log, gvisorCfg)
			if err != nil {
				log.Warn("failed to create gVisor provider", "error", err)
			} else {
				orchestrator.RegisterProvider(ProviderGVisor, gvisorProvider)
			}

			// Register Firecracker provider (requires KVM — skip if unavailable)
			fcProvider, err := NewFirecrackerProvider(log, &FirecrackerProviderConfig{})
			if err != nil {
				log.Warn("failed to create Firecracker provider", "error", err)
			} else if fcProvider.IsKVMAvailable() {
				orchestrator.RegisterProvider(ProviderFirecracker, fcProvider)
			} else {
				log.Info("Firecracker provider not registered — KVM not available")
			}

			// Register E2B provider (requires API key)
			if cfg.Workspace.E2BAPIKey != "" {
				e2bProvider, err := NewE2BProvider(log, &E2BProviderConfig{
					APIKey: cfg.Workspace.E2BAPIKey,
				})
				if err != nil {
					log.Warn("failed to create E2B provider", "error", err)
				} else {
					orchestrator.RegisterProvider(ProviderE2B, e2bProvider)
				}
			} else {
				log.Info("E2B provider not registered — E2B_API_KEY not set")
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

// startCleanupJob starts the cleanup job if agent workspaces are enabled.
func startCleanupJob(lc fx.Lifecycle, job *CleanupJob, cfg *config.Config, log *slog.Logger) {
	if !cfg.Workspace.IsEnabled() {
		return
	}

	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			job.Start(ctx)
			return nil
		},
		OnStop: func(_ context.Context) error {
			job.Stop()
			return nil
		},
	})
}

// newWarmPool creates a warm pool with configuration from env vars.
func newWarmPool(orchestrator *Orchestrator, log *slog.Logger, cfg *config.Config) *WarmPool {
	poolCfg := DefaultWarmPoolConfig()
	if cfg.Workspace.WarmPoolSize > 0 {
		poolCfg.Size = cfg.Workspace.WarmPoolSize
	}
	return NewWarmPool(orchestrator, log, poolCfg)
}

// startWarmPool initializes the warm pool on server start if enabled.
func startWarmPool(lc fx.Lifecycle, pool *WarmPool, cfg *config.Config, log *slog.Logger) {
	if !cfg.Workspace.IsEnabled() {
		return
	}

	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			// Slight delay to let providers register first
			go func() {
				time.Sleep(3 * time.Second)
				if err := pool.Start(context.Background()); err != nil {
					log.Error("failed to start warm pool", "error", err)
				}
			}()
			return nil
		},
		OnStop: func(ctx context.Context) error {
			return pool.Stop(ctx)
		},
	})
}

// newMCPHostingService creates the MCP hosting service.
func newMCPHostingService(store *Store, service *Service, orchestrator *Orchestrator, log *slog.Logger) *MCPHostingService {
	return NewMCPHostingService(store, service, orchestrator, log)
}

// newMCPHostingHandler creates the MCP hosting handler.
func newMCPHostingHandler(hosting *MCPHostingService, log *slog.Logger) *MCPHostingHandler {
	return NewMCPHostingHandler(hosting, log)
}

// startMCPHosting registers MCP hosting routes and manages server lifecycle.
func startMCPHosting(lc fx.Lifecycle, cfg *config.Config, e *echo.Echo, h *MCPHostingHandler, hosting *MCPHostingService, authMiddleware *auth.Middleware, log *slog.Logger) {
	if !cfg.Workspace.IsEnabled() {
		return
	}

	RegisterMCPHostingRoutes(e, h, authMiddleware, log)

	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			// Auto-start all persistent MCP servers from the database
			go func() {
				// Slight delay to let providers register first
				time.Sleep(2 * time.Second)
				if err := hosting.StartAll(context.Background()); err != nil {
					log.Error("failed to auto-start MCP servers", "error", err)
				}
			}()
			return nil
		},
		OnStop: func(ctx context.Context) error {
			// Graceful shutdown of all MCP servers
			return hosting.Shutdown(ctx)
		},
	})
}
