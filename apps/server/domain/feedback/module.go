package feedback

import (
	"go.uber.org/fx"
)

// Module provides user/agent feedback intake.
var Module = fx.Module("feedback",
	fx.Provide(NewService),
	fx.Provide(NewHandler),
	fx.Invoke(RegisterRoutes),
)
