package superadmin

import (
	"go.uber.org/fx"
)

// Module provides the superadmin domain
var Module = fx.Module("superadmin",
	fx.Provide(NewRepository),
	fx.Provide(NewHandler),
	fx.Invoke(RegisterRoutes),
)
