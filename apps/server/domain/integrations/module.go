package integrations

import (
	"go.uber.org/fx"
)

// Module provides the integrations domain
var Module = fx.Module("integrations",
	fx.Provide(NewRepository),
	fx.Provide(NewIntegrationRegistry),
	fx.Provide(NewHandler),
	fx.Invoke(RegisterRoutes),
)
