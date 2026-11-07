package backups

import (
	"go.uber.org/fx"
)

// Module provides the backups domain
var Module = fx.Module("backups",
	fx.Provide(NewRepository),
	fx.Provide(NewExporter),
	fx.Provide(NewImporter),
	fx.Provide(NewCreator),
	fx.Provide(NewRestorer),
	fx.Provide(NewService),
	fx.Provide(NewHandler),
	fx.Provide(NewScheduleRunner),
	fx.Provide(NewRetentionTask),
	fx.Invoke(RegisterRoutes),
	fx.Invoke(RegisterScheduledBackupTasks),
)
