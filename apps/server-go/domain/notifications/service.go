package notifications

import (
	"context"
	"log/slog"

	"github.com/emergent/emergent-core/pkg/logger"
)

// Service handles business logic for notifications
type Service struct {
	repo *Repository
	log  *slog.Logger
}

// NewService creates a new notifications service
func NewService(repo *Repository, log *slog.Logger) *Service {
	return &Service{
		repo: repo,
		log:  log.With(logger.Scope("notifications.svc")),
	}
}

// GetStats returns notification statistics for a user
func (s *Service) GetStats(ctx context.Context, userID string) (*NotificationStats, error) {
	return s.repo.GetStats(ctx, userID)
}

// GetCounts returns notification counts by tab for a user
func (s *Service) GetCounts(ctx context.Context, userID string) (*NotificationCounts, error) {
	return s.repo.GetCounts(ctx, userID)
}

// List returns notifications for a user with filters
func (s *Service) List(ctx context.Context, userID string, params ListParams) ([]Notification, error) {
	return s.repo.List(ctx, userID, params)
}

// MarkRead marks a notification as read
func (s *Service) MarkRead(ctx context.Context, userID, notificationID string) error {
	return s.repo.MarkRead(ctx, userID, notificationID)
}

// Dismiss dismisses a notification
func (s *Service) Dismiss(ctx context.Context, userID, notificationID string) error {
	return s.repo.Dismiss(ctx, userID, notificationID)
}

// MarkAllRead marks all notifications as read for a user
func (s *Service) MarkAllRead(ctx context.Context, userID string) (int64, error) {
	return s.repo.MarkAllRead(ctx, userID)
}