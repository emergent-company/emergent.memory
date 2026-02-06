package useractivity

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/emergent/emergent-core/pkg/apperror"
	"github.com/emergent/emergent-core/pkg/logger"
)

// Service handles business logic for user activity
type Service struct {
	repo *Repository
	log  *slog.Logger
}

// NewService creates a new user activity service
func NewService(repo *Repository, log *slog.Logger) *Service {
	return &Service{
		repo: repo,
		log:  log.With(logger.Scope("useractivity.svc")),
	}
}

// Record records a user activity
func (s *Service) Record(ctx context.Context, userID, projectID string, req *RecordActivityRequest) error {
	// Validate UUID formats
	if _, err := uuid.Parse(req.ResourceID); err != nil {
		return apperror.ErrBadRequest.WithMessage("Invalid resourceId format")
	}
	if _, err := uuid.Parse(projectID); err != nil {
		return apperror.ErrBadRequest.WithMessage("Invalid projectId format")
	}

	now := time.Now().UTC()
	item := &UserRecentItem{
		ID:              uuid.New().String(),
		UserID:          userID,
		ProjectID:       projectID,
		ResourceType:    req.ResourceType,
		ResourceID:      req.ResourceID,
		ResourceName:    req.ResourceName,
		ResourceSubtype: req.ResourceSubtype,
		ActionType:      req.ActionType,
		AccessedAt:      now,
		CreatedAt:       now,
	}

	return s.repo.Record(ctx, item)
}

// GetRecent retrieves recent activity items for a user
func (s *Service) GetRecent(ctx context.Context, userID string, limit int) (*RecentItemsResponse, error) {
	items, err := s.repo.GetRecent(ctx, userID, limit)
	if err != nil {
		return nil, err
	}

	return s.toResponse(items), nil
}

// GetRecentByType retrieves recent activity items filtered by resource type
func (s *Service) GetRecentByType(ctx context.Context, userID, resourceType string, limit int) (*RecentItemsResponse, error) {
	items, err := s.repo.GetRecentByType(ctx, userID, resourceType, limit)
	if err != nil {
		return nil, err
	}

	return s.toResponse(items), nil
}

// DeleteAll deletes all recent activity for a user
func (s *Service) DeleteAll(ctx context.Context, userID string) error {
	return s.repo.DeleteAll(ctx, userID)
}

// DeleteByResource deletes a specific resource from user's recent activity
func (s *Service) DeleteByResource(ctx context.Context, userID, resourceType, resourceID string) error {
	if _, err := uuid.Parse(resourceID); err != nil {
		return apperror.ErrBadRequest.WithMessage("Invalid resourceId format")
	}

	return s.repo.DeleteByResource(ctx, userID, resourceType, resourceID)
}

// toResponse converts entities to response format
func (s *Service) toResponse(items []UserRecentItem) *RecentItemsResponse {
	data := make([]RecentItemResponse, len(items))
	for i, item := range items {
		data[i] = RecentItemResponse{
			ID:              item.ID,
			ResourceType:    item.ResourceType,
			ResourceID:      item.ResourceID,
			ResourceName:    item.ResourceName,
			ResourceSubtype: item.ResourceSubtype,
			ActionType:      item.ActionType,
			AccessedAt:      item.AccessedAt,
			ProjectID:       item.ProjectID,
		}
	}

	return &RecentItemsResponse{Data: data}
}
