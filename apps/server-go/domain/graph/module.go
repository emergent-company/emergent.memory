package graph

import "go.uber.org/fx"

// Module provides graph domain dependencies.
var Module = fx.Module("graph",
	fx.Provide(NewRepository),
	fx.Provide(NewService),
	fx.Provide(NewHandler),
	fx.Invoke(RegisterRoutes),
)
