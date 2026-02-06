package typeregistry

import (
	"go.uber.org/fx"
)

// Module provides the type registry domain
var Module = fx.Module("typeregistry",
	fx.Provide(NewRepository),
	fx.Provide(NewHandler),
	fx.Invoke(RegisterRoutes),
)
