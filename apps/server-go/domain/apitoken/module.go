package apitoken

import "go.uber.org/fx"

// Module provides API token domain dependencies
var Module = fx.Module("apitoken",
	fx.Provide(NewRepository),
	fx.Provide(NewService),
	fx.Provide(NewHandler),
	fx.Invoke(RegisterRoutes),
)
