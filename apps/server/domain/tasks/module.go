package tasks

import (
	"go.uber.org/fx"
)

// Module provides the tasks domain
var Module = fx.Module("tasks",
	fx.Provide(NewRepository),
	fx.Provide(NewService),
	fx.Provide(NewHandler),
	fx.Invoke(RegisterRoutes),
)
