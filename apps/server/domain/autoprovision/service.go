package autoprovision

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/emergent-company/emergent.memory/domain/orgs"
	"github.com/emergent-company/emergent.memory/domain/projects"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
	"github.com/emergent-company/emergent.memory/pkg/logger"
)

const (
	// maxOrgNameRetries is the maximum number of suffix attempts for org name collisions.
	maxOrgNameRetries = 5
)

// orgCreator is the interface for org creation (satisfied by *orgs.Service).
type orgCreator interface {
	Create(ctx context.Context, name string, userID string) (*orgs.OrgDTO, error)
}

// projectCreator is the interface for project creation (satisfied by *projects.Service).
type projectCreator interface {
	Create(ctx context.Context, req projects.CreateProjectRequest, userID string) (*projects.ProjectDTO, error)
}

// Service implements auth.AutoProvisionService by creating a default org and project
// for newly registered users.
type Service struct {
	orgsSvc     orgCreator
	projectsSvc projectCreator
	log         *slog.Logger
}

// NewService creates a new auto-provision service.
func NewService(orgsSvc *orgs.Service, projectsSvc *projects.Service, log *slog.Logger) auth.AutoProvisionService {
	return &Service{
		orgsSvc:     orgsSvc,
		projectsSvc: projectsSvc,
		log:         log.With(logger.Scope("autoprovision")),
	}
}

// newServiceWithDeps creates a service with injected dependencies (for testing).
func newServiceWithDeps(orgsSvc orgCreator, projectsSvc projectCreator, log *slog.Logger) *Service {
	return &Service{
		orgsSvc:     orgsSvc,
		projectsSvc: projectsSvc,
		log:         log,
	}
}

// ProvisionNewUser creates a default org and project for a newly registered user.
func (s *Service) ProvisionNewUser(ctx context.Context, userID string, profile *auth.UserProfileInfo) error {
	orgName := deriveOrgName(profile)
	projectName := deriveProjectName(profile)

	s.log.Info("auto-provisioning new user",
		slog.String("userID", userID),
		slog.String("orgName", orgName),
		slog.String("projectName", projectName),
	)

	// Create org (with conflict/retry handling for duplicate names)
	org, err := s.createOrgWithRetry(ctx, orgName, userID)
	if err != nil {
		s.log.Error("auto-provision: failed to create org",
			slog.String("userID", userID),
			slog.String("orgName", orgName),
			logger.Error(err),
		)
		return fmt.Errorf("create org: %w", err)
	}

	s.log.Info("auto-provision: org created",
		slog.String("userID", userID),
		slog.String("orgID", org.ID),
		slog.String("orgName", org.Name),
	)

	// Create project within the new org — partial failure is logged but does not roll back the org
	_, err = s.projectsSvc.Create(ctx, projects.CreateProjectRequest{
		Name:  projectName,
		OrgID: org.ID,
	}, userID)
	if err != nil {
		s.log.Error("auto-provision: org created but project creation failed",
			slog.String("userID", userID),
			slog.String("orgID", org.ID),
			slog.String("projectName", projectName),
			logger.Error(err),
		)
		return fmt.Errorf("create project: %w", err)
	}

	s.log.Info("auto-provision: complete",
		slog.String("userID", userID),
		slog.String("orgID", org.ID),
	)

	return nil
}

// createOrgWithRetry attempts to create an org, appending numeric suffixes on name collision.
func (s *Service) createOrgWithRetry(ctx context.Context, baseName string, userID string) (*orgs.OrgDTO, error) {
	// First attempt with the base name
	org, err := s.orgsSvc.Create(ctx, baseName, userID)
	if err == nil {
		return org, nil
	}

	if !isConflictError(err) {
		return nil, err
	}

	// Retry with numeric suffix
	for i := 2; i <= maxOrgNameRetries+1; i++ {
		name := fmt.Sprintf("%s %d", baseName, i)
		org, err = s.orgsSvc.Create(ctx, name, userID)
		if err == nil {
			return org, nil
		}
		if !isConflictError(err) {
			return nil, err
		}
	}

	return nil, fmt.Errorf("org name conflict persists after %d attempts: %s", maxOrgNameRetries, baseName)
}

// deriveOrgName returns the org name using the fallback chain:
// "<FirstName> <LastName>'s Org" → "<DisplayName>'s Org" → "<email-local>'s Org" → "My Organization"
func deriveOrgName(profile *auth.UserProfileInfo) string {
	if profile == nil {
		return "My Organization"
	}

	first := strings.TrimSpace(profile.FirstName)
	last := strings.TrimSpace(profile.LastName)

	if first != "" && last != "" {
		return first + " " + last + "'s Org"
	}
	if first != "" {
		return first + "'s Org"
	}

	display := strings.TrimSpace(profile.DisplayName)
	if display != "" {
		return display + "'s Org"
	}

	if profile.Email != "" {
		local := emailLocalPart(profile.Email)
		if local != "" {
			return local + "'s Org"
		}
	}

	return "My Organization"
}

// deriveProjectName returns the project name using the fallback chain:
// "<FirstName>'s Project" → "<DisplayName>'s Project" → "<email-local>'s Project" → "My Project"
func deriveProjectName(profile *auth.UserProfileInfo) string {
	if profile == nil {
		return "My Project"
	}

	first := strings.TrimSpace(profile.FirstName)
	if first != "" {
		return first + "'s Project"
	}

	display := strings.TrimSpace(profile.DisplayName)
	if display != "" {
		return display + "'s Project"
	}

	if profile.Email != "" {
		local := emailLocalPart(profile.Email)
		if local != "" {
			return local + "'s Project"
		}
	}

	return "My Project"
}

// emailLocalPart returns the part before '@' in an email address.
func emailLocalPart(email string) string {
	at := strings.Index(email, "@")
	if at <= 0 {
		return ""
	}
	return email[:at]
}

// isConflictError checks if an error is a 409 conflict (e.g., duplicate name).
func isConflictError(err error) bool {
	if err == nil {
		return false
	}
	if appErr, ok := err.(*apperror.Error); ok {
		return appErr.HTTPStatus == 409
	}
	return false
}
