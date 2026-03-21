package journal

import (
	"go.uber.org/fx"
)

// Module provides the journal domain.
var Module = fx.Module("journal",
	fx.Provide(NewRepository),
	fx.Provide(NewService),
	fx.Provide(NewHandler),
	fx.Invoke(RegisterRoutes),
)
