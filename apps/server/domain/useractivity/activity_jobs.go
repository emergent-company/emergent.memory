package useractivity

import (
	"context"
	"log/slog"
	"time"

	"go.uber.org/fx"

	"github.com/emergent-company/emergent.memory/pkg/logger"
)

const (
	// activityRollupInterval is how often derived actions are recomputed. The
	// rollup is idempotent, so a missed tick is harmless.
	activityRollupInterval = time.Hour
	// activityRollupLookback covers late-arriving events (chat debounce, usage
	// flush) without rescanning all history.
	activityRollupLookback = 2 * 24 * time.Hour
	// activityBackfillWindow seeds the feature on a fresh deployment.
	activityBackfillWindow = 90 * 24 * time.Hour
	// activityRetention keeps a little over a year of daily buckets.
	activityRetention = 400 * 24 * time.Hour
)

// ActivityJobsParams are the dependencies for the activity rollup jobs.
type ActivityJobsParams struct {
	fx.In

	LC   fx.Lifecycle
	Repo *Repository
	Log  *slog.Logger
}

// RegisterActivityJobs runs the rollup/prune loop and the one-time backfill.
func RegisterActivityJobs(p ActivityJobsParams) {
	log := p.Log.With(logger.Scope("useractivity.jobs"))
	stop := make(chan struct{})

	p.LC.Append(fx.Hook{
		OnStart: func(context.Context) error {
			go runActivityJobs(p.Repo, log, stop)
			return nil
		},
		OnStop: func(context.Context) error {
			close(stop)
			return nil
		},
	})
}

func runActivityJobs(repo *Repository, log *slog.Logger, stop <-chan struct{}) {
	ctx := context.Background()
	now := time.Now().UTC()

	has, err := repo.HasActivity(ctx)
	if err != nil {
		log.Warn("activity backfill check failed; skipping", logger.Error(err))
	} else if !has {
		if err := repo.BackfillSeen(ctx, now.Add(-activityBackfillWindow)); err != nil {
			log.Warn("activity last-seen backfill failed", logger.Error(err))
		}
	}
	if err := repo.Rollup(ctx, now.Add(-activityRollupLookback)); err != nil {
		log.Warn("activity rollup failed", logger.Error(err))
	}
	if err := repo.Prune(ctx, now.Add(-activityRetention)); err != nil {
		log.Warn("activity prune failed", logger.Error(err))
	}

	ticker := time.NewTicker(activityRollupInterval)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			now = time.Now().UTC()
			if err := repo.Rollup(ctx, now.Add(-activityRollupLookback)); err != nil {
				log.Warn("activity rollup failed", logger.Error(err))
			}
			if err := repo.Prune(ctx, now.Add(-activityRetention)); err != nil {
				log.Warn("activity prune failed", logger.Error(err))
			}
		}
	}
}
