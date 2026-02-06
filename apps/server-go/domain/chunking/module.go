package chunking

import (
	"go.uber.org/fx"
)

var Module = fx.Module("chunking",
	fx.Provide(
		NewService,
		NewHandler,
	),
	fx.Invoke(RegisterRoutes),
)
