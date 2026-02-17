package tasks

import (
	"context"
	"log/slog"

	"github.com/emergent-company/emergent/pkg/apperror"
	"github.com/emergent-company/emergent/pkg/logger"
)

// Service handles business logic for tasks
type Service struct {
	repo *Repository
	log  *slog.Logger
}

// NewService creates a new tasks service
func NewService(repo *Repository, log *slog.Logger) *Service {
	return &Service{
		repo: repo,
		log:  log.With(logger.Scope("tasks.svc")),
	}
}

// GetCountsByProject returns task counts by status for a specific project
func (s *Service) GetCountsByProject(ctx context.Context, projectID string) (*TaskCounts, error) {
	return s.repo.GetCountsByProject(ctx, projectID)
}

// GetAllCounts returns task counts by status across all accessible projects
func (s *Service) GetAllCounts(ctx context.Context, userID string) (*TaskCounts, error) {
	return s.repo.GetAllCounts(ctx, userID)
}

// List returns tasks for a project with optional filters
func (s *Service) List(ctx context.Context, params TaskListParams) (*TaskListResponse, error) {
	tasks, total, err := s.repo.List(ctx, params)
	if err != nil {
		return nil, err
	}

	return &TaskListResponse{
		Data:  tasks,
		Total: total,
	}, nil
}

// ListAll returns tasks across all user-accessible projects
func (s *Service) ListAll(ctx context.Context, userID string, params TaskListParams) (*TaskListResponse, error) {
	tasks, total, err := s.repo.ListAll(ctx, userID, params)
	if err != nil {
		return nil, err
	}

	return &TaskListResponse{
		Data:  tasks,
		Total: total,
	}, nil
}

// GetByID retrieves a task by ID
func (s *Service) GetByID(ctx context.Context, projectID, taskID string) (*Task, error) {
	return s.repo.GetByID(ctx, projectID, taskID)
}

// Resolve resolves a task as accepted or rejected
func (s *Service) Resolve(ctx context.Context, projectID, taskID, userID string, req *ResolveTaskRequest) error {
	// Validate resolution
	if req.Resolution != "accepted" && req.Resolution != "rejected" {
		return apperror.ErrBadRequest.WithMessage("resolution must be 'accepted' or 'rejected'")
	}

	return s.repo.Resolve(ctx, projectID, taskID, userID, req.Resolution, req.ResolutionNotes)
}

// Cancel cancels a pending task
func (s *Service) Cancel(ctx context.Context, projectID, taskID, userID string) error {
	return s.repo.Cancel(ctx, projectID, taskID, userID)
}
