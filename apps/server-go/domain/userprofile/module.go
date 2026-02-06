package userprofile

import (
	"go.uber.org/fx"
)

// Module provides the user profile domain
var Module = fx.Module("userprofile",
	fx.Provide(NewRepository),
	fx.Provide(NewService),
	fx.Provide(NewHandler),
	fx.Invoke(RegisterRoutes),
)
