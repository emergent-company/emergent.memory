package scheduler

import (
	"context"
	"log/slog"
	"time"

	"github.com/uptrace/bun"
	"go.uber.org/fx"
)

// Module provides scheduled task functionality
var Module = fx.Module("scheduler",
	fx.Provide(
		NewConfig,
		NewScheduler,
		ProvideStaleJobCleanupTask,
	),
	fx.Invoke(
		RegisterTasks,
		RegisterSchedulerLifecycle,
	),
)

// staleTaskParams are the minimal deps needed to build the stale cleanup task.
type staleTaskParams struct {
	fx.In
	DB  *bun.DB
	Log *slog.Logger
	Cfg *Config
}

// ProvideStaleJobCleanupTask creates the stale job cleanup task and makes it
// available for injection by other modules (e.g. embedding control handler).
func ProvideStaleJobCleanupTask(p staleTaskParams) *StaleJobCleanupTask {
	return NewStaleJobCleanupTask(p.DB, p.Log, p.Cfg.StaleJobMinutes)
}

// TaskParams contains dependencies for creating scheduled tasks
type TaskParams struct {
	fx.In
	Scheduler    *Scheduler
	DB           *bun.DB
	Log          *slog.Logger
	Cfg          *Config
	StaleJobTask *StaleJobCleanupTask
}

// RegisterTasks registers all scheduled tasks
func RegisterTasks(p TaskParams) error {
	if !p.Cfg.Enabled {
		p.Log.Info("scheduler disabled, skipping task registration")
		return nil
	}

	// Register revision count refresh task
	revisionTask := NewRevisionCountRefreshTask(p.DB, p.Log)
	if err := addScheduledTask(p.Scheduler, p.Log, "revision_count_refresh",
		p.Cfg.RevisionCountRefreshSchedule, p.Cfg.RevisionCountRefreshInterval, revisionTask.Run); err != nil {
		p.Log.Error("failed to register revision count refresh task",
			slog.String("error", err.Error()))
	}

	// Register tag cleanup task
	tagCleanupTask := NewTagCleanupTask(p.DB, p.Log)
	if err := addScheduledTask(p.Scheduler, p.Log, "tag_cleanup",
		p.Cfg.TagCleanupSchedule, p.Cfg.TagCleanupInterval, tagCleanupTask.Run); err != nil {
		p.Log.Error("failed to register tag cleanup task",
			slog.String("error", err.Error()))
	}

	// Register cache cleanup task
	cacheCleanupTask := NewCacheCleanupTask(p.DB, p.Log)
	if err := addScheduledTask(p.Scheduler, p.Log, "cache_cleanup",
		p.Cfg.CacheCleanupSchedule, p.Cfg.CacheCleanupInterval, cacheCleanupTask.Run); err != nil {
		p.Log.Error("failed to register cache cleanup task",
			slog.String("error", err.Error()))
	}

	// Register stale job cleanup task
	if err := addScheduledTask(p.Scheduler, p.Log, "stale_job_cleanup",
		p.Cfg.StaleJobCleanupSchedule, p.Cfg.StaleJobCleanupInterval, p.StaleJobTask.Run); err != nil {
		p.Log.Error("failed to register stale job cleanup task",
			slog.String("error", err.Error()))
	}

	p.Log.Info("registered scheduled tasks",
		slog.Any("tasks", p.Scheduler.ListTasks()))

	return nil
}

// addScheduledTask registers a task using a cron schedule if provided, otherwise using an interval.
// The cron schedule takes precedence over the interval when both are specified.
// If the cron schedule is invalid, falls back to using the interval.
func addScheduledTask(s *Scheduler, log *slog.Logger, name, cronSchedule string, interval time.Duration, task TaskFunc) error {
	if cronSchedule != "" {
		log.Info("using cron schedule for task",
			slog.String("name", name),
			slog.String("schedule", cronSchedule))
		err := s.AddCronTask(name, cronSchedule, task)
		if err != nil {
			log.Warn("invalid cron schedule, falling back to interval",
				slog.String("name", name),
				slog.String("schedule", cronSchedule),
				slog.Duration("interval", interval),
				slog.String("error", err.Error()))
			return s.AddIntervalTask(name, interval, task)
		}
		return nil
	}
	return s.AddIntervalTask(name, interval, task)
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
