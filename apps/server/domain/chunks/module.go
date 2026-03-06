package chunks

import (
	"go.uber.org/fx"
)

// Module provides chunks dependencies via fx
var Module = fx.Module("chunks",
	fx.Provide(
		NewRepository,
		NewService,
		NewHandler,
	),
	fx.Invoke(RegisterRoutes),
)
