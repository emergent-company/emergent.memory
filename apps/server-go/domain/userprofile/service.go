package userprofile

import (
	"context"
	"log/slog"

	"github.com/emergent/emergent-core/pkg/logger"
)

// Service handles business logic for user profiles
type Service struct {
	repo *Repository
	log  *slog.Logger
}

// NewService creates a new user profile service
func NewService(repo *Repository, log *slog.Logger) *Service {
	return &Service{
		repo: repo,
		log:  log.With(logger.Scope("userprofile.svc")),
	}
}

// GetByID retrieves a user profile by internal ID and returns as DTO
func (s *Service) GetByID(ctx context.Context, id string) (*ProfileDTO, error) {
	profile, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	// Fetch email
	email, _ := s.repo.GetEmail(ctx, profile.ID)

	dto := profile.ToDTO(email)
	return &dto, nil
}

// GetByZitadelUserID retrieves a user profile by Zitadel user ID and returns as DTO
func (s *Service) GetByZitadelUserID(ctx context.Context, zitadelUserID string) (*ProfileDTO, error) {
	profile, err := s.repo.GetByZitadelUserID(ctx, zitadelUserID)
	if err != nil {
		return nil, err
	}

	// Fetch email
	email, _ := s.repo.GetEmail(ctx, profile.ID)

	dto := profile.ToDTO(email)
	return &dto, nil
}

// Update updates a user profile and returns the updated DTO
func (s *Service) Update(ctx context.Context, id string, req *UpdateProfileRequest) (*ProfileDTO, error) {
	profile, err := s.repo.Update(ctx, id, req)
	if err != nil {
		return nil, err
	}

	// Fetch email
	email, _ := s.repo.GetEmail(ctx, profile.ID)

	dto := profile.ToDTO(email)
	return &dto, nil
}
