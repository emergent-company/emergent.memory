package apitoken

import (
	"go.uber.org/fx"

	"github.com/emergent-company/emergent.memory/pkg/encryption"
)

// Module provides API token domain dependencies
var Module = fx.Module("apitoken",
	fx.Provide(encryption.NewService),
	fx.Provide(NewRepository),
	fx.Provide(NewService),
	fx.Provide(NewHandler),
	fx.Invoke(RegisterRoutes),
)
