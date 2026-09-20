package agents

import (
	"context"
	"log/slog"

	"go.uber.org/fx"

	"github.com/emergent-company/emergent.memory/domain/apitoken"
	"github.com/emergent-company/emergent.memory/domain/events"
	"github.com/emergent-company/emergent.memory/domain/mcp"
	"github.com/emergent-company/emergent.memory/domain/mcpregistry"
	"github.com/emergent-company/emergent.memory/domain/mcprelay"
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
		NewTestLLMFlagChecker,
		provideADKTestLLMChecker,
		provideEmbeddingsTestLLMChecker,
		provideToolPool,
		provideSessionService,
		provideAgentExecutor,
		provideHandler,
		provideACPHandler,
		provideA2AHandler,
		provideTriggerService,
		provideMCPToolHandler,
		provideWebhookRateLimiter,
		provideWorkerPool,
		provideStaleRunReaper,
		provideSessionTitleHandlerForMCP,
		provideOrgToolPoolInvalidator,
	),
	fx.Invoke(
		RegisterRoutes,
		RegisterACPRoutes,
		RegisterA2ARoutes,
		registerAgentTriggers,
		registerOrphanRecovery,
		registerWorkerPool,
		registerHandlerMCPToolHandler,
		registerHandlerMCPService,
		registerRelayToolPoolInvalidator,
		registerAgentToolHandler,
		registerToolPoolInvalidator,
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
func provideToolPool(mcpService *mcp.Service, registryService *mcpregistry.Service, relayService *mcprelay.Service, log *slog.Logger) *ToolPool {
	return NewToolPool(ToolPoolConfig{
		MCPService:      mcpService,
		RegistryService: registryService,
		RelayService:    relayService,
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
	eventsSvc *events.Service,
	log *slog.Logger,
) *AgentExecutor {
	return NewAgentExecutor(modelFactory, toolPool, repo, skillRepo, embeddingsSvc, provisioner, cfg, sessionService, providerRepo, apiTokenSvc, usageSvc, eventsSvc, log)
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
func provideACPHandler(repo *Repository, executor *AgentExecutor, eventsSvc *events.Service, log *slog.Logger) *ACPHandler {
	return NewACPHandler(repo, executor, eventsSvc, log)
}

// provideA2AHandler creates an A2AHandler from fx dependencies.
func provideA2AHandler(repo *Repository, executor *AgentExecutor, eventsSvc *events.Service, log *slog.Logger, cfg *config.Config) *A2AHandler {
	return NewA2AHandler(repo, executor, eventsSvc, log, cfg)
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
func provideMCPToolHandler(repo *Repository, executor *AgentExecutor, log *slog.Logger, extractionJobs mcp.ExtractionJobFinder, embeddingJobs mcp.EmbeddingJobFinder) *MCPToolHandler {
	return NewMCPToolHandler(repo, executor, log, extractionJobs, embeddingJobs)
}

// registerAgentToolHandler wires the MCPToolHandler into mcp.Service after
// construction (agents → mcp; deferred to fx.Invoke to break the constructor cycle).
func registerAgentToolHandler(mcpService *mcp.Service, handler *MCPToolHandler) {
	mcpService.RegisterAgentToolHandler(handler)
}

// registerHandlerMCPToolHandler injects the MCPToolHandler into the REST Handler
// so the project-scoped run route can reuse buildRememberStatus for the
// remember-status endpoint.
func registerHandlerMCPToolHandler(h *Handler, mcpToolHandler *MCPToolHandler) {
	h.WithMCPToolHandler(mcpToolHandler)
}

// registerHandlerMCPService injects the MCP service into the REST Handler so the
// agent-definition read path can compute the full tool-group catalog (dynamic
// tool scope resolution) without a per-request DB scan.
func registerHandlerMCPService(h *Handler, mcpService *mcp.Service) {
	h.WithMCPService(mcpService)
}

// provideSessionTitleHandlerForMCP exposes the Repository as mcp.SessionTitleHandler
// so the set_session_title built-in tool can update session metadata.
func provideSessionTitleHandlerForMCP(repo *Repository) mcp.SessionTitleHandler {
	return repo
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

// registerToolPoolInvalidator wires the ToolPool into mcpregistry.Service after
// construction (mcpregistry → agents; deferred to fx.Invoke to break the constructor cycle).
func registerToolPoolInvalidator(registryService *mcpregistry.Service, toolPool *ToolPool) {
	registryService.RegisterToolPoolInvalidator(toolPool)
}

// provideOrgToolPoolInvalidator exposes the ToolPool as orgs.ToolPoolInvalidator
// so org-level tool setting changes automatically invalidate the ToolPool cache.
func provideOrgToolPoolInvalidator(toolPool *ToolPool) orgs.ToolPoolInvalidator {
	return toolPool
}

// registerRelayToolPoolInvalidator wires the ToolPool into the mcprelay service
// so that relay session registrations/unregistrations automatically invalidate
// the ToolPool cache for the affected project.
func registerRelayToolPoolInvalidator(relayService *mcprelay.Service, toolPool *ToolPool) {
	relayService.OnChange(func(projectID, instanceID string, isRegister bool) {
		toolPool.InvalidateCache(projectID)
	})
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

// provideADKTestLLMChecker exposes the test-LLM flag checker to pkg/adk.
func provideADKTestLLMChecker(c *TestLLMFlagChecker) adk.TestLLMChecker {
	return c
}

// provideEmbeddingsTestLLMChecker exposes the test-LLM flag checker to pkg/embeddings.
func provideEmbeddingsTestLLMChecker(c *TestLLMFlagChecker) embeddings.TestLLMChecker {
	return c
}
