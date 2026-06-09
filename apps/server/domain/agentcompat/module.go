package agentcompat

import (
	"go.uber.org/fx"
)

// Module wires the agentcompat domain.
// It depends on the agents domain being active (agents.Repository + agents.AgentExecutor).
var Module = fx.Module("agentcompat",
	fx.Provide(
		NewService,
		NewHandler,
	),
	fx.Invoke(RegisterRoutes),
)
