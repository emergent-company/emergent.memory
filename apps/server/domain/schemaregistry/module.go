package schemaregistry

import (
	"go.uber.org/fx"
)

// Module provides the schema registry domain
var Module = fx.Module("schemaregistry",
	fx.Provide(NewRepository),
	fx.Provide(NewHandler),
	fx.Invoke(RegisterRoutes),
)
