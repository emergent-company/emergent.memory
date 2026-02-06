package templatepacks

import (
	"go.uber.org/fx"
)

// Module provides the template packs domain
var Module = fx.Module("templatepacks",
	fx.Provide(NewRepository),
	fx.Provide(NewService),
	fx.Provide(NewHandler),
	fx.Invoke(RegisterRoutes),
)
