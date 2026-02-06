package tasks

import (
	"context"
	"database/sql"
	"log/slog"
	"time"

	"github.com/uptrace/bun"

	"github.com/emergent/emergent-core/pkg/apperror"
	"github.com/emergent/emergent-core/pkg/logger"
)

// Repository handles database operations for tasks
type Repository struct {
	db  bun.IDB
	log *slog.Logger
}

// NewRepository creates a new tasks repository
func NewRepository(db bun.IDB, log *slog.Logger) *Repository {
	return &Repository{
		db:  db,
		log: log.With(logger.Scope("tasks.repo")),
	}
}

// GetCountsByProject returns task counts by status for a specific project
func (r *Repository) GetCountsByProject(ctx context.Context, projectID string) (*TaskCounts, error) {
	counts := &TaskCounts{}

	// Count pending
	pending, err := r.db.NewSelect().
		Model((*Task)(nil)).
		Where("project_id = ?", projectID).
		Where("status = ?", "pending").
		Count(ctx)
	if err != nil {
		r.log.Error("failed to count pending tasks", logger.Error(err))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}
	counts.Pending = int64(pending)

	// Count accepted
	accepted, err := r.db.NewSelect().
		Model((*Task)(nil)).
		Where("project_id = ?", projectID).
		Where("status = ?", "accepted").
		Count(ctx)
	if err != nil {
		r.log.Error("failed to count accepted tasks", logger.Error(err))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}
	counts.Accepted = int64(accepted)

	// Count rejected
	rejected, err := r.db.NewSelect().
		Model((*Task)(nil)).
		Where("project_id = ?", projectID).
		Where("status = ?", "rejected").
		Count(ctx)
	if err != nil {
		r.log.Error("failed to count rejected tasks", logger.Error(err))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}
	counts.Rejected = int64(rejected)

	// Count cancelled
	cancelled, err := r.db.NewSelect().
		Model((*Task)(nil)).
		Where("project_id = ?", projectID).
		Where("status = ?", "cancelled").
		Count(ctx)
	if err != nil {
		r.log.Error("failed to count cancelled tasks", logger.Error(err))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}
	counts.Cancelled = int64(cancelled)

	return counts, nil
}

// List returns tasks for a project with optional filters
func (r *Repository) List(ctx context.Context, params TaskListParams) ([]Task, int, error) {
	if params.Limit <= 0 {
		params.Limit = 50
	}
	if params.Limit > 200 {
		params.Limit = 200
	}

	tasks := []Task{}
	q := r.db.NewSelect().
		Model(&tasks).
		Where("project_id = ?", params.ProjectID)

	// Apply status filter
	if params.Status != "" {
		q = q.Where("status = ?", params.Status)
	}

	// Apply type filter
	if params.Type != "" {
		q = q.Where("type = ?", params.Type)
	}

	// Get total count
	totalQ := r.db.NewSelect().
		Model((*Task)(nil)).
		Where("project_id = ?", params.ProjectID)
	if params.Status != "" {
		totalQ = totalQ.Where("status = ?", params.Status)
	}
	if params.Type != "" {
		totalQ = totalQ.Where("type = ?", params.Type)
	}
	total, err := totalQ.Count(ctx)
	if err != nil {
		r.log.Error("failed to count tasks", logger.Error(err))
		return nil, 0, apperror.ErrDatabase.WithInternal(err)
	}

	// Apply pagination and ordering
	q = q.Order("created_at DESC").
		Limit(params.Limit).
		Offset(params.Offset)

	if err := q.Scan(ctx); err != nil {
		r.log.Error("failed to list tasks", logger.Error(err))
		return nil, 0, apperror.ErrDatabase.WithInternal(err)
	}

	return tasks, total, nil
}

// ListAll returns tasks across all user-accessible projects
func (r *Repository) ListAll(ctx context.Context, userID string, params TaskListParams) ([]Task, int, error) {
	if params.Limit <= 0 {
		params.Limit = 50
	}
	if params.Limit > 200 {
		params.Limit = 200
	}

	tasks := []Task{}
	q := r.db.NewSelect().
		Model(&tasks).
		Join("INNER JOIN kb.project_memberships pm ON pm.project_id = t.project_id").
		Where("pm.user_id = ?", userID)

	// Apply status filter
	if params.Status != "" {
		q = q.Where("t.status = ?", params.Status)
	}

	// Apply type filter
	if params.Type != "" {
		q = q.Where("t.type = ?", params.Type)
	}

	// Get total count
	totalQ := r.db.NewSelect().
		Model((*Task)(nil)).
		Join("INNER JOIN kb.project_memberships pm ON pm.project_id = t.project_id").
		Where("pm.user_id = ?", userID)
	if params.Status != "" {
		totalQ = totalQ.Where("t.status = ?", params.Status)
	}
	if params.Type != "" {
		totalQ = totalQ.Where("t.type = ?", params.Type)
	}
	total, err := totalQ.Count(ctx)
	if err != nil {
		r.log.Error("failed to count all tasks", logger.Error(err))
		return nil, 0, apperror.ErrDatabase.WithInternal(err)
	}

	// Apply pagination and ordering
	q = q.Order("t.created_at DESC").
		Limit(params.Limit).
		Offset(params.Offset)

	if err := q.Scan(ctx); err != nil {
		r.log.Error("failed to list all tasks", logger.Error(err))
		return nil, 0, apperror.ErrDatabase.WithInternal(err)
	}

	return tasks, total, nil
}

// GetByID retrieves a task by ID
func (r *Repository) GetByID(ctx context.Context, projectID, taskID string) (*Task, error) {
	var task Task
	err := r.db.NewSelect().
		Model(&task).
		Where("id = ?", taskID).
		Where("project_id = ?", projectID).
		Scan(ctx)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, apperror.ErrNotFound.WithMessage("Task not found")
		}
		r.log.Error("failed to get task", logger.Error(err))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}

	return &task, nil
}

// Resolve updates a task's status to accepted or rejected
func (r *Repository) Resolve(ctx context.Context, projectID, taskID, userID, resolution string, notes *string) error {
	now := time.Now()

	result, err := r.db.NewUpdate().
		Model((*Task)(nil)).
		Set("status = ?", resolution).
		Set("resolved_at = ?", now).
		Set("resolved_by = ?", userID).
		Set("resolution_notes = ?", notes).
		Set("updated_at = ?", now).
		Where("id = ?", taskID).
		Where("project_id = ?", projectID).
		Where("status = ?", "pending"). // Can only resolve pending tasks
		Exec(ctx)

	if err != nil {
		r.log.Error("failed to resolve task", logger.Error(err))
		return apperror.ErrDatabase.WithInternal(err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return apperror.ErrNotFound.WithMessage("Task not found or already resolved")
	}

	return nil
}

// Cancel updates a task's status to cancelled
func (r *Repository) Cancel(ctx context.Context, projectID, taskID, userID string) error {
	now := time.Now()

	result, err := r.db.NewUpdate().
		Model((*Task)(nil)).
		Set("status = ?", "cancelled").
		Set("resolved_at = ?", now).
		Set("resolved_by = ?", userID).
		Set("updated_at = ?", now).
		Where("id = ?", taskID).
		Where("project_id = ?", projectID).
		Where("status = ?", "pending"). // Can only cancel pending tasks
		Exec(ctx)

	if err != nil {
		r.log.Error("failed to cancel task", logger.Error(err))
		return apperror.ErrDatabase.WithInternal(err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return apperror.ErrNotFound.WithMessage("Task not found or already resolved")
	}

	return nil
}

// GetAllCounts returns task counts by status across all user-accessible projects
func (r *Repository) GetAllCounts(ctx context.Context, userID string) (*TaskCounts, error) {
	counts := &TaskCounts{}

	// Count pending
	pending, err := r.db.NewSelect().
		Model((*Task)(nil)).
		Join("INNER JOIN kb.project_memberships pm ON pm.project_id = t.project_id").
		Where("pm.user_id = ?", userID).
		Where("t.status = ?", "pending").
		Count(ctx)
	if err != nil {
		r.log.Error("failed to count all pending tasks", logger.Error(err))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}
	counts.Pending = int64(pending)

	// Count accepted
	accepted, err := r.db.NewSelect().
		Model((*Task)(nil)).
		Join("INNER JOIN kb.project_memberships pm ON pm.project_id = t.project_id").
		Where("pm.user_id = ?", userID).
		Where("t.status = ?", "accepted").
		Count(ctx)
	if err != nil {
		r.log.Error("failed to count all accepted tasks", logger.Error(err))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}
	counts.Accepted = int64(accepted)

	// Count rejected
	rejected, err := r.db.NewSelect().
		Model((*Task)(nil)).
		Join("INNER JOIN kb.project_memberships pm ON pm.project_id = t.project_id").
		Where("pm.user_id = ?", userID).
		Where("t.status = ?", "rejected").
		Count(ctx)
	if err != nil {
		r.log.Error("failed to count all rejected tasks", logger.Error(err))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}
	counts.Rejected = int64(rejected)

	// Count cancelled
	cancelled, err := r.db.NewSelect().
		Model((*Task)(nil)).
		Join("INNER JOIN kb.project_memberships pm ON pm.project_id = t.project_id").
		Where("pm.user_id = ?", userID).
		Where("t.status = ?", "cancelled").
		Count(ctx)
	if err != nil {
		r.log.Error("failed to count all cancelled tasks", logger.Error(err))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}
	counts.Cancelled = int64(cancelled)

	return counts, nil
}
