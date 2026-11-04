package docs

import (
	"go.uber.org/fx"
)

var Module = fx.Module("documentation",
	fx.Provide(NewService),
	fx.Provide(NewHandler),
	fx.Invoke(RegisterRoutes),
)
