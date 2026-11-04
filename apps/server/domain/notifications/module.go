package notifications

import (
	"go.uber.org/fx"
)

// Module provides the notifications domain
var Module = fx.Module("notifications",
	fx.Provide(NewRepository),
	fx.Provide(NewService),
	fx.Provide(NewHandler),
	fx.Invoke(RegisterRoutes),
)
