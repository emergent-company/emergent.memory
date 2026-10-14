package schemas

import (
	"go.uber.org/fx"
)

// Module provides the schemas domain
var Module = fx.Module("schemas",
	fx.Provide(NewRepository),
	fx.Provide(NewService),
	fx.Provide(NewHandler),
	fx.Provide(NewSchemaMigrationJobWorker),
	fx.Invoke(RegisterRoutes),
	fx.Invoke(RegisterSchemaMigrationWorkerLifecycle),
)
