package backups

import (
	"go.uber.org/fx"
)

// Module provides the backups domain
var Module = fx.Module("backups",
	fx.Provide(NewRepository),
	fx.Provide(NewExporter),
	fx.Provide(NewCreator),
	fx.Provide(NewService),
	fx.Provide(NewHandler),
	fx.Invoke(RegisterRoutes),
)
