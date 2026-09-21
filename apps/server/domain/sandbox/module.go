package sandbox

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/uptrace/bun"
	"go.uber.org/fx"

	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// Module provides workspace dependencies.
// All workspace functionality is gated behind the ENABLE_AGENT_SANDBOXES feature flag.
var Module = fx.Options(
	fx.Provide(newStoreFromDB),
	fx.Provide(newServiceFromConfig),
	fx.Provide(newOrchestrator),
	fx.Provide(newCleanupJob),
	fx.Provide(newReconciler),
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
	if !cfg.Sandbox.IsEnabled() {
		log.Info("agent sandboxes disabled (ENABLE_AGENT_SANDBOXES=false), skipping route registration")
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
	if cfg.Sandbox.MaxConcurrent > 0 {
		svc.config.MaxConcurrent = cfg.Sandbox.MaxConcurrent
	}
	if cfg.Sandbox.DefaultTTLDays > 0 {
		svc.config.DefaultTTLDays = cfg.Sandbox.DefaultTTLDays
	}
	if cfg.Sandbox.DefaultProvider != "" {
		svc.config.DefaultProvider = ProviderType(cfg.Sandbox.DefaultProvider)
	}
	if cfg.Sandbox.DefaultCPU != "" {
		svc.config.DefaultCPU = cfg.Sandbox.DefaultCPU
	}
	if cfg.Sandbox.DefaultMemory != "" {
		svc.config.DefaultMemory = cfg.Sandbox.DefaultMemory
	}
	if cfg.Sandbox.DefaultDisk != "" {
		svc.config.DefaultDisk = cfg.Sandbox.DefaultDisk
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

// autoProvisionerParams bundles dependencies for newAutoProvisioner.
type autoProvisionerParams struct {
	fx.In

	Service       *Service
	Orchestrator  *Orchestrator
	CheckoutSvc   *CheckoutService
	SetupExec     *SetupExecutor
	WarmPool      *WarmPool
	Cfg           *config.Config
	Log           *slog.Logger
	ImageResolver ImageResolver `optional:"true"`
}

// newAutoProvisioner creates the auto-provisioning service for agent sandboxes.
func newAutoProvisioner(p autoProvisionerParams) *AutoProvisioner {
	return NewAutoProvisioner(p.Service, p.Orchestrator, p.CheckoutSvc, p.SetupExec, p.WarmPool, p.Log, p.ImageResolver)
}

// newCleanupJob creates a cleanup job with configuration from env vars.
func newCleanupJob(store *Store, orchestrator *Orchestrator, mcpHosting *MCPHostingService, log *slog.Logger, cfg *config.Config) *CleanupJob {
	cleanupCfg := DefaultCleanupConfig()
	if cfg.Sandbox.MaxConcurrent > 0 {
		cleanupCfg.MaxConcurrent = cfg.Sandbox.MaxConcurrent
	}
	if cfg.Sandbox.CleanupIntervalMin > 0 {
		cleanupCfg.Interval = time.Duration(cfg.Sandbox.CleanupIntervalMin) * time.Minute
	}
	if cfg.Sandbox.AlertThresholdPct > 0 && cfg.Sandbox.AlertThresholdPct <= 100 {
		cleanupCfg.AlertThreshold = float64(cfg.Sandbox.AlertThresholdPct) / 100.0
	}
	// 0 (the default) leaves the idle reclamation policy disabled.
	cleanupCfg.PersistentIdleTTLDays = cfg.Sandbox.PersistentIdleTTLDays
	return NewCleanupJob(store, orchestrator, mcpHosting, log, cleanupCfg)
}

// newReconciler creates the label-driven orphan reconciler from configuration.
func newReconciler(store *Store, orchestrator *Orchestrator, log *slog.Logger, cfg *config.Config) *Reconciler {
	grace := time.Duration(cfg.Sandbox.ReconcileGraceMin) * time.Minute
	rec := NewReconciler(orchestrator, store, grace, log)
	if cfg.Sandbox.OwnerHeartbeatMin > 0 {
		rec.heartbeatTTL = ownerHeartbeatTTLMultiplier * time.Duration(cfg.Sandbox.OwnerHeartbeatMin) * time.Minute
	}
	return rec
}

// registerProviders registers all available workspace providers with the orchestrator.
func registerProviders(lc fx.Lifecycle, orchestrator *Orchestrator, rec *Reconciler, cfg *config.Config, log *slog.Logger) {
	if !cfg.Sandbox.IsEnabled() {
		return
	}

	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			// Register gVisor provider (always available — cross-platform fallback)
			gvisorCfg := &GVisorProviderConfig{
				NetworkName:  cfg.Sandbox.NetworkName,
				DefaultImage: cfg.Sandbox.DefaultImage,
			}
			gvisorProvider, err := NewGVisorProvider(log, gvisorCfg)
			if err != nil {
				orchestrator.MarkUnavailable(ProviderGVisor, "gVisor (Docker)", fmt.Sprintf("provider construction failed: %v", err))
				log.Warn("failed to create gVisor provider", "error", err)
			} else {
				orchestrator.RegisterProvider(ProviderGVisor, gvisorProvider)
			}

			// Register Firecracker provider (requires KVM — skip if unavailable)
			fcProvider, err := NewFirecrackerProvider(log, &FirecrackerProviderConfig{})
			if err != nil {
				orchestrator.MarkUnavailable(ProviderFirecracker, "Firecracker", err.Error())
				log.Warn("failed to create Firecracker provider", "error", err)
			} else if fcProvider.IsKVMAvailable() {
				orchestrator.RegisterProvider(ProviderFirecracker, fcProvider)
			} else {
				orchestrator.MarkUnavailable(ProviderFirecracker, "Firecracker", "KVM not available on this host")
				log.Info("Firecracker provider not registered — KVM not available")
			}

			// Register E2B provider (requires API key)
			if cfg.Sandbox.E2BAPIKey != "" {
				e2bProvider, err := NewE2BProvider(log, &E2BProviderConfig{
					APIKey: cfg.Sandbox.E2BAPIKey,
				})
				if err != nil {
					orchestrator.MarkUnavailable(ProviderE2B, "E2B", err.Error())
					log.Warn("failed to create E2B provider", "error", err)
				} else {
					orchestrator.RegisterProvider(ProviderE2B, e2bProvider)
				}
			} else {
				orchestrator.MarkUnavailable(ProviderE2B, "E2B", "E2B_API_KEY not set")
				log.Info("E2B provider not registered — E2B_API_KEY not set")
			}

			// Start health monitoring
			orchestrator.StartHealthMonitoring(ctx)

			// Reclaim predecessors' orphans immediately, without waiting for the
			// cleanup interval. Runs after providers are registered so the gVisor
			// provider can enumerate Docker resources. Gated on IsEnabled().
			reconcileAtStartup(cfg, rec, log)

			return nil
		},
		OnStop: func(_ context.Context) error {
			orchestrator.StopHealthMonitoring()
			return nil
		},
	})
}

// startCleanupJob starts the cleanup job if agent sandboxes are enabled.
func startCleanupJob(lc fx.Lifecycle, job *CleanupJob, rec *Reconciler, cfg *config.Config, log *slog.Logger) {
	if !cfg.Sandbox.IsEnabled() {
		return
	}

	// Reconcile ownerless sandbox resources on every cleanup cycle, alongside TTL expiry.
	job.SetReconciler(rec, cfg.Sandbox.ReconcileEnabled)

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
	// WORKSPACE_WARM_POOL_SIZE overrides the compiled-in default in both directions
	// (set higher to increase pool, set to 0 to disable).
	// envDefault:"2" means the env var is always present, so always apply it.
	poolCfg.Size = cfg.Sandbox.WarmPoolSize
	if cfg.Sandbox.WarmPoolTargetImage != "" {
		poolCfg.TargetImage = cfg.Sandbox.WarmPoolTargetImage
	}
	if cfg.Sandbox.WarmPoolExtraImages != "" {
		for _, img := range strings.Split(cfg.Sandbox.WarmPoolExtraImages, ",") {
			img = strings.TrimSpace(img)
			if img != "" {
				poolCfg.ExtraImages = append(poolCfg.ExtraImages, img)
			}
		}
	}
	pool := NewWarmPool(orchestrator, log, poolCfg)
	if cfg.Sandbox.OwnerHeartbeatMin > 0 {
		pool.heartbeatInterval = time.Duration(cfg.Sandbox.OwnerHeartbeatMin) * time.Minute
	}
	return pool
}

// startWarmPool initializes the warm pool on server start if enabled.
func startWarmPool(lc fx.Lifecycle, pool *WarmPool, cfg *config.Config, log *slog.Logger) {
	if !cfg.Sandbox.IsEnabled() {
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
	if !cfg.Sandbox.IsEnabled() {
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
