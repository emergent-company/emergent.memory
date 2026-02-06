package users

import (
	"context"
	"log/slog"

	"github.com/emergent/emergent-core/pkg/apperror"
	"github.com/emergent/emergent-core/pkg/logger"
)

// Service handles business logic for users
type Service struct {
	repo *Repository
	log  *slog.Logger
}

// NewService creates a new users service
func NewService(repo *Repository, log *slog.Logger) *Service {
	return &Service{
		repo: repo,
		log:  log.With(logger.Scope("users.svc")),
	}
}

// SearchByEmail searches for users by email (partial match)
// Requires at least 2 characters in the query
func (s *Service) SearchByEmail(ctx context.Context, emailQuery string, excludeUserID *string) (*UserSearchResponse, error) {
	if len(emailQuery) < 2 {
		return nil, apperror.ErrBadRequest.WithMessage("email query must be at least 2 characters")
	}

	results, err := s.repo.SearchByEmail(ctx, emailQuery, excludeUserID)
	if err != nil {
		return nil, err
	}

	// Ensure we return empty slice, not nil
	if results == nil {
		results = []UserSearchResult{}
	}

	return &UserSearchResponse{
		Users: results,
	}, nil
}

// FindByEmail finds a user by exact email match
func (s *Service) FindByEmail(ctx context.Context, email string) (*UserSearchResult, error) {
	return s.repo.FindByEmail(ctx, email)
}
