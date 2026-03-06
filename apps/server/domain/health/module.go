package health

import (
	"go.uber.org/fx"
)

var Module = fx.Module("health",
	fx.Provide(
		NewHandler,
		NewMetricsHandler,
	),
	fx.Invoke(RegisterRoutes),
)
