package processingproblems

import "go.uber.org/fx"

// Module provides the processing-problems read model.
var Module = fx.Module("processingproblems",
	fx.Provide(NewRepository),
	fx.Provide(NewService),
	fx.Provide(NewHandler),
	fx.Invoke(RegisterRoutes),
)
