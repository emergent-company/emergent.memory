package users

import (
	"go.uber.org/fx"
)

// Module provides the users domain
var Module = fx.Module("users",
	fx.Provide(NewRepository),
	fx.Provide(NewService),
	fx.Provide(NewHandler),
	fx.Invoke(RegisterRoutes),
)
