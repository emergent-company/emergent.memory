package agents

import (
	"go.uber.org/fx"
)

// Module provides the agents domain
var Module = fx.Module("agents",
	fx.Provide(NewRepository),
	fx.Provide(NewHandler),
	fx.Invoke(RegisterRoutes),
)
