package sessiontodos

import "go.uber.org/fx"

// Module provides the session todos domain.
var Module = fx.Module("sessiontodos",
	fx.Provide(NewRepository),
	fx.Provide(NewService),
	fx.Provide(NewHandler),
	fx.Invoke(RegisterRoutes),
)
