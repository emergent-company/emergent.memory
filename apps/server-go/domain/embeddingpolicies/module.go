package embeddingpolicies

import (
	"go.uber.org/fx"
)

var Module = fx.Module("embeddingpolicies",
	fx.Provide(
		NewStore,
		NewService,
		NewHandler,
	),
	fx.Invoke(RegisterRoutes),
)
