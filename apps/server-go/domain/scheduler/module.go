package scheduler

import (
	"context"
	"log/slog"

	"github.com/uptrace/bun"
	"go.uber.org/fx"
)

// Module provides scheduled task functionality
var Module = fx.Module("scheduler",
	fx.Provide(
		NewConfig,
		NewScheduler,
	),
	fx.Invoke(
		RegisterTasks,
		RegisterSchedulerLifecycle,
	),
)

// TaskParams contains dependencies for creating scheduled tasks
type TaskParams struct {
	fx.In
	Scheduler *Scheduler
	DB        *bun.DB
	Log       *slog.Logger
	Cfg       *Config
}

// RegisterTasks registers all scheduled tasks
func RegisterTasks(p TaskParams) error {
	if !p.Cfg.Enabled {
		p.Log.Info("scheduler disabled, skipping task registration")
		return nil
	}

	// Register revision count refresh task
	revisionTask := NewRevisionCountRefreshTask(p.DB, p.Log)
	if err := p.Scheduler.AddIntervalTask("revision_count_refresh",
		p.Cfg.RevisionCountRefreshInterval, revisionTask.Run); err != nil {
		p.Log.Error("failed to register revision count refresh task",
			slog.String("error", err.Error()))
	}

	// Register tag cleanup task
	tagCleanupTask := NewTagCleanupTask(p.DB, p.Log)
	if err := p.Scheduler.AddIntervalTask("tag_cleanup",
		p.Cfg.TagCleanupInterval, tagCleanupTask.Run); err != nil {
		p.Log.Error("failed to register tag cleanup task",
			slog.String("error", err.Error()))
	}

	// Register cache cleanup task
	cacheCleanupTask := NewCacheCleanupTask(p.DB, p.Log)
	if err := p.Scheduler.AddIntervalTask("cache_cleanup",
		p.Cfg.CacheCleanupInterval, cacheCleanupTask.Run); err != nil {
		p.Log.Error("failed to register cache cleanup task",
			slog.String("error", err.Error()))
	}

	// Register stale job cleanup task
	staleJobTask := NewStaleJobCleanupTask(p.DB, p.Log, p.Cfg.StaleJobMinutes)
	if err := p.Scheduler.AddIntervalTask("stale_job_cleanup",
		p.Cfg.StaleJobCleanupInterval, staleJobTask.Run); err != nil {
		p.Log.Error("failed to register stale job cleanup task",
			slog.String("error", err.Error()))
	}

	p.Log.Info("registered scheduled tasks",
		slog.Any("tasks", p.Scheduler.ListTasks()))

	return nil
}

// RegisterSchedulerLifecycle registers the scheduler with fx lifecycle
func RegisterSchedulerLifecycle(lc fx.Lifecycle, scheduler *Scheduler, cfg *Config) {
	if !cfg.Enabled {
		return
	}

	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			return scheduler.Start(ctx)
		},
		OnStop: func(ctx context.Context) error {
			return scheduler.Stop(ctx)
		},
	})
}
