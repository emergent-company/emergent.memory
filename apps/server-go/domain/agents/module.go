package agents

import (
	"context"
	"log/slog"

	"go.uber.org/fx"

	"github.com/emergent/emergent-core/domain/mcp"
	"github.com/emergent/emergent-core/domain/scheduler"
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
	),
	fx.Invoke(
		RegisterRoutes,
		registerAgentTriggers,
	),
)

// provideToolPool creates a ToolPool from fx dependencies.
func provideToolPool(mcpService *mcp.Service, log *slog.Logger) *ToolPool {
	return NewToolPool(ToolPoolConfig{
		MCPService: mcpService,
		Logger:     log,
	})
}

// provideAgentExecutor creates an AgentExecutor from fx dependencies.
func provideAgentExecutor(
	modelFactory *adk.ModelFactory,
	toolPool *ToolPool,
	repo *Repository,
	log *slog.Logger,
) *AgentExecutor {
	return NewAgentExecutor(modelFactory, toolPool, repo, log)
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
