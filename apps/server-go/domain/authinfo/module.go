package authinfo

import (
	"go.uber.org/fx"
)

var Module = fx.Module("authinfo",
	fx.Provide(NewHandler),
	fx.Invoke(RegisterRoutes),
)
