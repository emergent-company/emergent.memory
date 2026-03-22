package schemas

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"go.uber.org/fx"

	"github.com/emergent-company/emergent.memory/pkg/logger"
)

const (
	migrationWorkerPollInterval = 5 * time.Second
	migrationWorkerBatchSize    = 5
)

// SchemaMigrationJobWorker polls kb.schema_migration_jobs for pending jobs
// and executes them by calling ExecuteSchemaMigration for each hop in the chain.
type SchemaMigrationJobWorker struct {
	svc *Service
	log *slog.Logger
	mu  sync.Mutex

	stopCh    chan struct{}
	stoppedCh chan struct{}
	running   bool
}

// NewSchemaMigrationJobWorker creates a new migration job worker.
func NewSchemaMigrationJobWorker(svc *Service, log *slog.Logger) *SchemaMigrationJobWorker {
	return &SchemaMigrationJobWorker{
		svc: svc,
		log: log.With(logger.Scope("schemas.migration.worker")),
	}
}

// Start begins the worker's polling loop.
func (w *SchemaMigrationJobWorker) Start(ctx context.Context) error {
	w.mu.Lock()
	if w.running {
		w.mu.Unlock()
		return nil
	}
	w.running = true
	w.stopCh = make(chan struct{})
	w.stoppedCh = make(chan struct{})
	w.mu.Unlock()

	w.log.Info("schema migration worker starting",
		slog.Duration("poll_interval", migrationWorkerPollInterval))

	go w.run(context.Background())
	return nil
}

// Stop signals the worker to stop and waits for it to finish.
func (w *SchemaMigrationJobWorker) Stop(_ context.Context) error {
	w.mu.Lock()
	if !w.running {
		w.mu.Unlock()
		return nil
	}
	close(w.stopCh)
	w.mu.Unlock()
	<-w.stoppedCh
	w.log.Info("schema migration worker stopped")
	return nil
}

func (w *SchemaMigrationJobWorker) run(ctx context.Context) {
	defer func() {
		w.mu.Lock()
		w.running = false
		w.mu.Unlock()
		close(w.stoppedCh)
	}()

	ticker := time.NewTicker(migrationWorkerPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-w.stopCh:
			return
		case <-ticker.C:
			w.processBatch(ctx)
		}
	}
}

// processBatch picks up pending jobs and executes them.
func (w *SchemaMigrationJobWorker) processBatch(ctx context.Context) {
	db := w.svc.repo.DB()

	// Fetch pending jobs
	type pendingJob struct {
		ID        string `bun:"id"`
		ProjectID string `bun:"project_id"`
	}
	var rows []pendingJob
	err := db.NewRaw(`
		SELECT id, project_id
		FROM kb.schema_migration_jobs
		WHERE status = 'pending'
		ORDER BY created_at ASC
		LIMIT ?
	`, migrationWorkerBatchSize).Scan(ctx, &rows)
	if err != nil {
		w.log.Error("failed to fetch pending migration jobs", logger.Error(err))
		return
	}

	for _, row := range rows {
		w.executeJob(ctx, row.ID, row.ProjectID)
	}
}

// executeJob runs all hops of a single migration job sequentially.
func (w *SchemaMigrationJobWorker) executeJob(ctx context.Context, jobID, projectID string) {
	job, err := w.svc.repo.GetMigrationJob(ctx, jobID)
	if err != nil {
		w.log.Error("failed to get migration job", slog.String("jobId", jobID), logger.Error(err))
		return
	}
	if job.Status != "pending" {
		return // Another worker may have claimed it
	}

	// Mark as running
	now := time.Now()
	job.Status = "running"
	job.StartedAt = &now
	if updateErr := w.svc.repo.UpdateMigrationJob(ctx, job); updateErr != nil {
		w.log.Error("failed to mark job as running", slog.String("jobId", jobID), logger.Error(updateErr))
		return
	}

	w.log.Info("executing schema migration job",
		slog.String("jobId", jobID),
		slog.String("projectId", projectID),
		slog.Int("hops", len(job.Chain)))

	// Execute each hop in order
	var jobErr error
	for _, hop := range job.Chain {
		resp, execErr := w.svc.ExecuteSchemaMigration(ctx, projectID, &SchemaMigrationExecuteRequest{
			FromSchemaID: hop.FromSchemaID,
			ToSchemaID:   hop.ToSchemaID,
			Hints:        hop.Hints,
			Force:        false,
		})
		if execErr != nil {
			jobErr = execErr
			w.log.Error("migration hop failed",
				slog.String("jobId", jobID),
				slog.String("fromSchemaId", hop.FromSchemaID),
				slog.String("toSchemaId", hop.ToSchemaID),
				logger.Error(execErr))
			break
		}
		job.ObjectsMigrated += resp.ObjectsMigrated
		job.ObjectsFailed += resp.ObjectsFailed
	}

	// Mark job complete or failed
	completedAt := time.Now()
	job.CompletedAt = &completedAt

	if jobErr != nil {
		job.Status = "failed"
		errStr := jobErr.Error()
		job.Error = &errStr
	} else {
		job.Status = "completed"

		// Task 7.5: Auto-uninstall from_version schema if requested
		if job.AutoUninstall && len(job.Chain) > 0 {
			fromSchemaID := job.Chain[0].FromSchemaID
			fromSchema, fetchErr := w.svc.repo.GetPackByID(ctx, fromSchemaID)
			if fetchErr == nil && fromSchema != nil {
				// Find and remove the project assignment for the from schema
				w.log.Info("auto-uninstalling from_schema after migration",
					slog.String("fromSchemaId", fromSchemaID),
					slog.String("projectId", projectID))
				// Soft-delete any active assignment for the from schema
				if _, delErr := w.svc.repo.DB().NewRaw(`
					UPDATE kb.project_schemas
					SET removed_at = NOW(), updated_at = NOW()
					WHERE project_id = ? AND schema_id = ? AND removed_at IS NULL
				`, projectID, fromSchemaID).Exec(ctx); delErr != nil {
					w.log.Warn("failed to auto-uninstall from_schema",
						slog.String("fromSchemaId", fromSchemaID),
						logger.Error(delErr))
				}
			}
		}
	}

	if updateErr := w.svc.repo.UpdateMigrationJob(ctx, job); updateErr != nil {
		w.log.Error("failed to update final job status",
			slog.String("jobId", jobID),
			logger.Error(updateErr))
	}
}

// ---------------------------------------------------------------------------
// fx wiring helpers (task 7.4)
// ---------------------------------------------------------------------------

// RegisterSchemaMigrationWorkerLifecycle registers the worker with fx lifecycle.
func RegisterSchemaMigrationWorkerLifecycle(lc fx.Lifecycle, worker *SchemaMigrationJobWorker) {
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			return worker.Start(context.Background())
		},
		OnStop: func(ctx context.Context) error {
			return worker.Stop(ctx)
		},
	})
}
