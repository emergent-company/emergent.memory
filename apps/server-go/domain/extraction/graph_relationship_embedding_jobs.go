package extraction

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent/pkg/logger"
)

// GraphRelationshipEmbeddingJobsService manages the relationship embedding job queue.
// Mirrors GraphEmbeddingJobsService but operates on kb.graph_relationships.
type GraphRelationshipEmbeddingJobsService struct {
	db  bun.IDB
	log *slog.Logger
	cfg *GraphEmbeddingConfig // reuse same config shape
}

// GraphRelationshipEmbeddingJob represents a job in kb.graph_relationship_embedding_jobs.
type GraphRelationshipEmbeddingJob struct {
	bun.BaseModel `bun:"table:kb.graph_relationship_embedding_jobs,alias:grej"`

	ID             string     `bun:"id,pk,type:uuid,default:gen_random_uuid()"`
	RelationshipID string     `bun:"relationship_id,notnull,type:uuid"`
	Status         JobStatus  `bun:"status,notnull,default:'pending'"`
	Priority       int        `bun:"priority,notnull,default:0"`
	AttemptCount   int        `bun:"attempt_count,notnull,default:0"`
	LastError      *string    `bun:"last_error"`
	ScheduledAt    time.Time  `bun:"scheduled_at,notnull"`
	StartedAt      *time.Time `bun:"started_at"`
	CompletedAt    *time.Time `bun:"completed_at"`
	CreatedAt      time.Time  `bun:"created_at,notnull"`
	UpdatedAt      time.Time  `bun:"updated_at,notnull"`
}

// NewGraphRelationshipEmbeddingJobsService creates a new service.
func NewGraphRelationshipEmbeddingJobsService(db bun.IDB, log *slog.Logger, cfg *GraphEmbeddingConfig) *GraphRelationshipEmbeddingJobsService {
	if cfg == nil {
		cfg = DefaultGraphEmbeddingConfig()
	}
	return &GraphRelationshipEmbeddingJobsService{
		db:  db,
		log: log.With(logger.Scope("graph.rel.embedding.jobs")),
		cfg: cfg,
	}
}

// Enqueue creates a new job or returns the existing active job for the relationship.
// Idempotent: if an active (pending|processing) job exists, returns it.
func (s *GraphRelationshipEmbeddingJobsService) Enqueue(ctx context.Context, relationshipID string) (*GraphRelationshipEmbeddingJob, error) {
	existing := &GraphRelationshipEmbeddingJob{}
	err := s.db.NewSelect().
		Model(existing).
		Where("relationship_id = ?", relationshipID).
		Where("status IN ('pending', 'processing')").
		Limit(1).
		Scan(ctx)

	if err == nil {
		return existing, nil
	}
	if err != sql.ErrNoRows {
		return nil, fmt.Errorf("check existing rel embedding job: %w", err)
	}

	job := &GraphRelationshipEmbeddingJob{
		RelationshipID: relationshipID,
		Status:         JobStatusPending,
		AttemptCount:   0,
		Priority:       0,
		ScheduledAt:    time.Now(),
	}

	_, err = s.db.NewInsert().
		Model(job).
		Returning("*").
		Exec(ctx)
	if err != nil {
		return nil, fmt.Errorf("enqueue rel embedding job: %w", err)
	}

	s.log.Debug("enqueued relationship embedding job",
		slog.String("job_id", job.ID),
		slog.String("relationship_id", job.RelationshipID))

	return job, nil
}

// Dequeue atomically claims up to batchSize jobs for processing.
func (s *GraphRelationshipEmbeddingJobsService) Dequeue(ctx context.Context, batchSize int) ([]*GraphRelationshipEmbeddingJob, error) {
	if batchSize <= 0 {
		batchSize = s.cfg.WorkerBatchSize
	}

	var jobs []*GraphRelationshipEmbeddingJob
	now := time.Now()

	_, err := s.db.NewRaw(`
		UPDATE kb.graph_relationship_embedding_jobs
		SET status = 'processing',
		    started_at = ?,
		    updated_at = ?,
		    attempt_count = attempt_count + 1
		WHERE id IN (
			SELECT id FROM kb.graph_relationship_embedding_jobs
			WHERE status = 'pending'
			  AND scheduled_at <= ?
			ORDER BY priority DESC, scheduled_at ASC
			LIMIT ?
			FOR UPDATE SKIP LOCKED
		)
		RETURNING *`,
		now, now, now, batchSize,
	).Exec(ctx, &jobs)

	if err != nil {
		return nil, fmt.Errorf("dequeue rel embedding jobs: %w", err)
	}
	return jobs, nil
}

// MarkCompleted marks a job as successfully completed.
func (s *GraphRelationshipEmbeddingJobsService) MarkCompleted(ctx context.Context, jobID string) error {
	now := time.Now()
	_, err := s.db.NewUpdate().
		TableExpr("kb.graph_relationship_embedding_jobs").
		Set("status = 'completed'").
		Set("completed_at = ?", now).
		Set("updated_at = ?", now).
		Where("id = ?", jobID).
		Exec(ctx)
	return err
}

// MarkFailed marks a job as failed and schedules a retry with exponential backoff.
func (s *GraphRelationshipEmbeddingJobsService) MarkFailed(ctx context.Context, jobID string, jobErr error) error {
	errMsg := jobErr.Error()
	now := time.Now()

	// Read current attempt count for backoff calculation
	var job GraphRelationshipEmbeddingJob
	if err := s.db.NewSelect().Model(&job).Where("id = ?", jobID).Scan(ctx); err != nil {
		return fmt.Errorf("fetch job for retry: %w", err)
	}

	delaySec := math.Min(
		float64(s.cfg.BaseRetryDelaySec)*math.Pow(2, float64(job.AttemptCount-1)),
		float64(s.cfg.MaxRetryDelaySec),
	)
	nextSchedule := now.Add(time.Duration(delaySec) * time.Second)

	_, err := s.db.NewUpdate().
		TableExpr("kb.graph_relationship_embedding_jobs").
		Set("status = 'pending'").
		Set("last_error = ?", errMsg).
		Set("scheduled_at = ?", nextSchedule).
		Set("updated_at = ?", now).
		Where("id = ?", jobID).
		Exec(ctx)
	return err
}

// RecoverStaleJobs resets processing jobs that appear stuck (started > 10 minutes ago).
func (s *GraphRelationshipEmbeddingJobsService) RecoverStaleJobs(ctx context.Context, limit int) (int, error) {
	staleThreshold := time.Now().Add(-10 * time.Minute)
	now := time.Now()

	result, err := s.db.NewUpdate().
		TableExpr("kb.graph_relationship_embedding_jobs").
		Set("status = 'pending'").
		Set("scheduled_at = ?", now).
		Set("updated_at = ?", now).
		Where("status = 'processing'").
		Where("started_at < ?", staleThreshold).
		Exec(ctx)
	if err != nil {
		return 0, fmt.Errorf("recover stale rel embedding jobs: %w", err)
	}
	n, _ := result.RowsAffected()
	return int(n), nil
}
