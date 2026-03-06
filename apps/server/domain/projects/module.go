package projects

import (
	"go.uber.org/fx"
)

// Module provides the projects domain
var Module = fx.Module("projects",
	fx.Provide(NewRepository),
	fx.Provide(NewService),
	fx.Provide(NewHandler),
	fx.Invoke(RegisterRoutes),
)
