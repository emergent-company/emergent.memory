package agents

import (
	"context"
	"log/slog"

	"go.uber.org/fx"

	"github.com/emergent-company/emergent.memory/domain/apitoken"
	"github.com/emergent-company/emergent.memory/domain/events"
	"github.com/emergent-company/emergent.memory/domain/mcp"
	"github.com/emergent-company/emergent.memory/domain/mcpregistry"
	"github.com/emergent-company/emergent.memory/domain/orgs"
	"github.com/emergent-company/emergent.memory/domain/provider"
	"github.com/emergent-company/emergent.memory/domain/sandbox"
	"github.com/emergent-company/emergent.memory/domain/scheduler"
	"github.com/emergent-company/emergent.memory/domain/skills"
	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/pkg/adk"
	"github.com/emergent-company/emergent.memory/pkg/adk/session/bunsession"
	"github.com/emergent-company/emergent.memory/pkg/embeddings"
	"github.com/uptrace/bun"
	"google.golang.org/adk/session"
)

// Module provides the agents domain
var Module = fx.Module("agents",
	fx.Provide(
		NewRepository,
		provideToolPool,
		provideSessionService,
		provideAgentExecutor,
		provideHandler,
		provideACPHandler,
		provideTriggerService,
		provideMCPToolHandler,
		provideWebhookRateLimiter,
		provideWorkerPool,
		provideStaleRunReaper,
	),
	fx.Invoke(
		RegisterRoutes,
		RegisterACPRoutes,
		registerAgentTriggers,
		registerOrphanRecovery,
		registerWorkerPool,
		registerAgentToolHandler,
		registerSessionTitleHandler,
		registerToolPoolInvalidator,
		registerOrgToolPoolInvalidator,
		registerStaleRunReaper,
	),
)

func provideSessionService(db *bun.DB) session.Service {
	return bunsession.NewService(db)
}

// provideWebhookRateLimiter creates a WebhookRateLimiter
func provideWebhookRateLimiter() *WebhookRateLimiter {
	return NewWebhookRateLimiter()
}

// provideToolPool creates a ToolPool from fx dependencies.
func provideToolPool(mcpService *mcp.Service, registryService *mcpregistry.Service, log *slog.Logger) *ToolPool {
	return NewToolPool(ToolPoolConfig{
		MCPService:      mcpService,
		RegistryService: registryService,
		Logger:          log,
	})
}

// provideAgentExecutor creates an AgentExecutor from fx dependencies.
func provideAgentExecutor(
	modelFactory *adk.ModelFactory,
	toolPool *ToolPool,
	repo *Repository,
	skillRepo *skills.Repository,
	embeddingsSvc *embeddings.Service,
	provisioner *sandbox.AutoProvisioner,
	cfg *config.Config,
	sessionService session.Service,
	providerRepo *provider.Repository,
	apiTokenSvc *apitoken.Service,
	usageSvc *provider.UsageService,
	log *slog.Logger,
) *AgentExecutor {
	return NewAgentExecutor(modelFactory, toolPool, repo, skillRepo, embeddingsSvc, provisioner, cfg, sessionService, providerRepo, apiTokenSvc, usageSvc, log)
}

// provideHandler creates a Handler with both repo and executor.
func provideHandler(repo *Repository, executor *AgentExecutor, rateLimiter *WebhookRateLimiter, cfg *config.Config, providerRepo *provider.Repository, usageSvc *provider.UsageService, sandboxStore *sandbox.Store) *Handler {
	tempoURL := ""
	if cfg.Otel.Enabled() {
		tempoURL = cfg.Otel.InternalTempoQueryURL()
	}
	return NewHandler(repo, executor, rateLimiter, tempoURL, &providerPricingAdapter{repo: providerRepo}, usageSvc, providerRepo, sandboxStore)
}

// providerPricingAdapter wraps *provider.Repository to satisfy the pricingLookup
// interface without importing the provider package from handler.go.
type providerPricingAdapter struct {
	repo *provider.Repository
}

func (a *providerPricingAdapter) lookupModelPricing(ctx context.Context, model string) (providerName string, textIn float64, out float64, found bool) {
	p, err := a.repo.GetPricingByModel(ctx, model)
	if err != nil || p == nil {
		return "", 0, 0, false
	}
	return string(p.Provider), p.TextInputPrice, p.OutputPrice, true
}

// provideACPHandler creates an ACPHandler from fx dependencies.
func provideACPHandler(repo *Repository, executor *AgentExecutor, log *slog.Logger) *ACPHandler {
	return NewACPHandler(repo, executor, log)
}

// provideTriggerService creates a TriggerService from fx dependencies.
func provideTriggerService(
	sched *scheduler.Scheduler,
	executor *AgentExecutor,
	repo *Repository,
	eventService *events.Service,
	log *slog.Logger,
) *TriggerService {
	return NewTriggerService(sched, executor, repo, eventService, log)
}

// provideMCPToolHandler creates an MCPToolHandler from fx dependencies.
func provideMCPToolHandler(repo *Repository, executor *AgentExecutor, log *slog.Logger) *MCPToolHandler {
	return NewMCPToolHandler(repo, executor, log)
}

// registerAgentToolHandler injects the MCPToolHandler into the MCP Service
// via setter injection to break the circular dependency (agents → mcp).
func registerAgentToolHandler(mcpService *mcp.Service, handler *MCPToolHandler) {
	mcpService.SetAgentToolHandler(handler)
}

// registerSessionTitleHandler injects the Repository (as SessionTitleHandler) into
// the MCP Service so the set_session_title built-in tool can update session metadata.
func registerSessionTitleHandler(mcpService *mcp.Service, repo *Repository) {
	mcpService.SetSessionTitleHandler(repo)
}

// registerOrphanRecovery marks any agent runs that were left in "running" status
// (due to an unclean server shutdown) as errored on startup, and re-enqueues
// any queued runs that lost their job row.
func registerOrphanRecovery(lc fx.Lifecycle, repo *Repository, log *slog.Logger) {
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			n, err := repo.MarkOrphanedRunsAsError(ctx)
			if err != nil {
				log.Warn("failed to mark orphaned agent runs as error on startup",
					slog.String("error", err.Error()),
				)
				return nil // best-effort, don't block startup
			}
			if n > 0 {
				log.Warn("marked orphaned agent runs as error on startup",
					slog.Int("count", n),
				)
			}

			// Re-enqueue queued runs that lost their job row (e.g. due to crash mid-enqueue)
			m, err := repo.RequeueOrphanedQueuedRuns(ctx)
			if err != nil {
				log.Warn("failed to re-enqueue orphaned queued runs on startup",
					slog.String("error", err.Error()),
				)
				return nil // best-effort
			}
			if m > 0 {
				log.Warn("re-enqueued orphaned queued runs on startup",
					slog.Int("count", m),
				)
			}
			return nil
		},
	})
}

// registerAgentTriggers syncs all agent triggers on startup.
func registerAgentTriggers(lc fx.Lifecycle, ts *TriggerService) {
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			// SyncAllTriggers is best-effort; log but don't block startup
			if err := ts.SyncAllTriggers(ctx); err != nil {
				ts.log.Warn("failed to sync agent triggers on startup",
					slog.String("error", err.Error()),
				)
			}
			return nil
		},
	})
}

// registerToolPoolInvalidator injects the ToolPool into the MCP registry service
// so that registry mutations (create/update/delete server, sync/toggle tools)
// automatically invalidate the ToolPool cache for the affected project.
func registerToolPoolInvalidator(registryService *mcpregistry.Service, toolPool *ToolPool) {
	registryService.SetToolPoolInvalidator(toolPool)
}

// registerOrgToolPoolInvalidator injects the ToolPool into the orgs service
// so that org-level tool setting changes automatically invalidate the ToolPool cache.
func registerOrgToolPoolInvalidator(orgService *orgs.Service, toolPool *ToolPool) {
	orgService.SetToolPoolInvalidator(toolPool)
}

// provideWorkerPool creates a WorkerPool from fx dependencies.
func provideWorkerPool(repo *Repository, executor *AgentExecutor, cfg *config.Config, log *slog.Logger) *WorkerPool {
	return NewWorkerPool(repo, executor, log, cfg.AgentWorkerPoolSize, cfg.AgentWorkerPollInterval)
}

// registerWorkerPool wires the WorkerPool into the fx lifecycle.
func registerWorkerPool(lc fx.Lifecycle, pool *WorkerPool) {
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			// Use context.Background() — the fx lifecycle ctx is cancelled after
			// OnStart returns, which would immediately kill all worker goroutines.
			return pool.Start(context.Background())
		},
		OnStop: func(ctx context.Context) error {
			pool.Stop()
			return nil
		},
	})
}

func provideStaleRunReaper(repo *Repository, log *slog.Logger) *StaleRunReaper {
	return NewStaleRunReaper(repo, log)
}

func registerStaleRunReaper(lc fx.Lifecycle, reaper *StaleRunReaper) {
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			reaper.Start(ctx)
			return nil
		},
		OnStop: func(ctx context.Context) error {
			reaper.Stop()
			return nil
		},
	})
}
