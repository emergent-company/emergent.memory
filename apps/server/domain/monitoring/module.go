package monitoring

import (
	"go.uber.org/fx"
)

// Module provides the monitoring domain
var Module = fx.Module("monitoring",
	fx.Provide(NewRepository),
	fx.Provide(NewHandler),
	fx.Invoke(RegisterRoutes),
)
