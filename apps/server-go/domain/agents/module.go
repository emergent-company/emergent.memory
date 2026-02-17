package agents

import (
	"context"
	"log/slog"

	"go.uber.org/fx"

	"github.com/emergent/emergent-core/domain/mcp"
	"github.com/emergent/emergent-core/domain/mcpregistry"
	"github.com/emergent/emergent-core/domain/scheduler"
	"github.com/emergent/emergent-core/domain/workspace"
	"github.com/emergent/emergent-core/internal/config"
	"github.com/emergent/emergent-core/pkg/adk"
)

// Module provides the agents domain
var Module = fx.Module("agents",
	fx.Provide(
		NewRepository,
		provideToolPool,
		provideAgentExecutor,
		provideHandler,
		provideTriggerService,
		provideMCPToolHandler,
	),
	fx.Invoke(
		RegisterRoutes,
		registerAgentTriggers,
		registerAgentToolHandler,
		registerToolPoolInvalidator,
	),
)

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
	provisioner *workspace.AutoProvisioner,
	cfg *config.Config,
	log *slog.Logger,
) *AgentExecutor {
	return NewAgentExecutor(modelFactory, toolPool, repo, provisioner, cfg, log)
}

// provideHandler creates a Handler with both repo and executor.
func provideHandler(repo *Repository, executor *AgentExecutor) *Handler {
	return NewHandler(repo, executor)
}

// provideTriggerService creates a TriggerService from fx dependencies.
func provideTriggerService(
	sched *scheduler.Scheduler,
	executor *AgentExecutor,
	repo *Repository,
	log *slog.Logger,
) *TriggerService {
	return NewTriggerService(sched, executor, repo, log)
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
