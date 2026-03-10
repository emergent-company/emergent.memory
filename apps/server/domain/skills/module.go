package skills

import (
	"go.uber.org/fx"
)

// Module provides the skills domain.
var Module = fx.Module("skills",
	fx.Provide(NewRepository),
	fx.Provide(NewHandler),
	fx.Invoke(RegisterRoutes),
)
