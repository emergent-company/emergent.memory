package orgs

import (
	"go.uber.org/fx"
)

// Module provides the organizations domain
var Module = fx.Module("orgs",
	fx.Provide(NewRepository),
	fx.Provide(NewService),
	fx.Provide(NewHandler),
	fx.Invoke(RegisterRoutes),
)
