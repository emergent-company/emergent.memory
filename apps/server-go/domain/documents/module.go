package documents

import (
	"go.uber.org/fx"
)

var Module = fx.Module("documents",
	fx.Provide(
		NewRepository,
		NewService,
		NewHandler,
		NewUploadHandler,
	),
	fx.Invoke(RegisterRoutes),
)
