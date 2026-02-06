package useractivity

import (
	"go.uber.org/fx"
)

// Module provides the user activity domain
var Module = fx.Module("useractivity",
	fx.Provide(NewRepository),
	fx.Provide(NewService),
	fx.Provide(NewHandler),
	fx.Invoke(RegisterRoutes),
)
