package orgs

import (
	"context"
	"log/slog"
	"strings"

	"go.uber.org/fx"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/logger"
)

const (
	// MaxOrgsPerUser is the maximum number of organizations a user can create
	MaxOrgsPerUser = 100
	// MaxOrgNameLength is the maximum length of an organization name
	MaxOrgNameLength = 120
)

// orgRepository is the persistence surface the service depends on. Declared as
// an interface so unit tests can substitute an in-memory double (the concrete
// *Repository is still what fx provides through ServiceParams).
type orgRepository interface {
	List(ctx context.Context, userID string) ([]OrgDTO, error)
	GetByID(ctx context.Context, id string) (*Org, error)
	Create(ctx context.Context, name, userID string) (*Org, error)
	UpdateName(ctx context.Context, id, name string) (*Org, error)
	Delete(ctx context.Context, id string) (bool, error)
	ListMembers(ctx context.Context, orgID string) ([]OrgMemberDTO, error)
	CountUserMemberships(ctx context.Context, userID string) (int, error)
	IsUserMember(ctx context.Context, orgID, userID string) (bool, error)
	FindOrgToolSettings(ctx context.Context, orgID string) ([]OrgToolSetting, error)
	UpsertOrgToolSetting(ctx context.Context, setting *OrgToolSetting) (*OrgToolSetting, error)
	DeleteOrgToolSetting(ctx context.Context, orgID, toolName string) (bool, error)
}

// Compile-time check that the concrete repository satisfies the service port.
var _ orgRepository = (*Repository)(nil)

// Service handles business logic for organizations
type Service struct {
	repo                orgRepository
	log                 *slog.Logger
	toolPoolInvalidator ToolPoolInvalidator
}

// ServiceParams bundles dependencies for NewService.
type ServiceParams struct {
	fx.In

	Repo *Repository
	Log  *slog.Logger

	// Optional cross-domain dependency (nil-safe when the agents feature is off).
	ToolPoolInvalidator ToolPoolInvalidator `optional:"true"`
}

// NewService creates a new organization service
func NewService(p ServiceParams) *Service {
	return &Service{
		repo:                p.Repo,
		log:                 p.Log.With(logger.Scope("orgs.svc")),
		toolPoolInvalidator: p.ToolPoolInvalidator,
	}
}

// List returns all organizations the user is a member of
func (s *Service) List(ctx context.Context, userID string) ([]OrgDTO, error) {
	if userID == "" {
		return []OrgDTO{}, nil
	}
	return s.repo.List(ctx, userID)
}

// GetByID returns an organization by ID
func (s *Service) GetByID(ctx context.Context, id string) (*OrgDTO, error) {
	org, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	dto := org.ToDTO()
	return &dto, nil
}

// normalizeOrgName trims and validates an organization name, mirroring the
// min=1/max=120 constraint expressed on the request DTOs.
func normalizeOrgName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", apperror.ErrBadRequest.WithMessage("Organization name is required")
	}
	if len(name) > MaxOrgNameLength {
		return "", apperror.ErrBadRequest.WithMessage("Organization name must be at most 120 characters")
	}
	return name, nil
}

// Create creates a new organization
func (s *Service) Create(ctx context.Context, name string, userID string) (*OrgDTO, error) {
	// Validate and sanitize name
	name, err := normalizeOrgName(name)
	if err != nil {
		return nil, err
	}

	// Check user's organization limit
	if userID != "" {
		count, err := s.repo.CountUserMemberships(ctx, userID)
		if err != nil {
			return nil, err
		}
		if count >= MaxOrgsPerUser {
			return nil, apperror.New(409, "conflict", "Organization limit reached (100). You can create up to 100 organizations.")
		}
	}

	// Create the organization
	org, err := s.repo.Create(ctx, name, userID)
	if err != nil {
		return nil, err
	}

	s.log.Info("organization created",
		slog.String("orgID", org.ID),
		slog.String("name", org.Name),
		slog.String("userID", userID))

	dto := org.ToDTO()
	return &dto, nil
}

// Update renames an organization and returns the updated DTO. Unknown (or
// soft-deleted) orgs surface the repository's not-found error unchanged.
func (s *Service) Update(ctx context.Context, id, name string) (*OrgDTO, error) {
	name, err := normalizeOrgName(name)
	if err != nil {
		return nil, err
	}

	org, err := s.repo.UpdateName(ctx, id, name)
	if err != nil {
		return nil, err
	}

	s.log.Info("organization renamed",
		slog.String("orgID", org.ID),
		slog.String("name", org.Name))

	dto := org.ToDTO()
	return &dto, nil
}

// Delete deletes an organization by ID
func (s *Service) Delete(ctx context.Context, id string) error {
	deleted, err := s.repo.Delete(ctx, id)
	if err != nil {
		return err
	}
	if !deleted {
		return apperror.ErrNotFound.WithMessage("Organization not found")
	}

	s.log.Info("organization deleted", slog.String("orgID", id))
	return nil
}

// ListMembers returns all members of an organization
func (s *Service) ListMembers(ctx context.Context, orgID string) ([]OrgMemberDTO, error) {
	return s.repo.ListMembers(ctx, orgID)
}
