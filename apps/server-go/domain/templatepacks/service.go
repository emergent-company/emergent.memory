package templatepacks

import (
	"context"
	"log/slog"

	"github.com/emergent/emergent-core/pkg/logger"
)

// Service handles business logic for template packs
type Service struct {
	repo *Repository
	log  *slog.Logger
}

// NewService creates a new template packs service
func NewService(repo *Repository, log *slog.Logger) *Service {
	return &Service{
		repo: repo,
		log:  log.With(logger.Scope("templatepacks.svc")),
	}
}

// GetCompiledTypes returns compiled object and relationship types for a project
func (s *Service) GetCompiledTypes(ctx context.Context, projectID string) (*CompiledTypesResponse, error) {
	return s.repo.GetCompiledTypesByProject(ctx, projectID)
}

// GetAvailablePacks returns template packs available for a project to install
func (s *Service) GetAvailablePacks(ctx context.Context, projectID string) ([]TemplatePackListItem, error) {
	return s.repo.GetAvailablePacks(ctx, projectID)
}

// GetInstalledPacks returns template packs installed for a project
func (s *Service) GetInstalledPacks(ctx context.Context, projectID string) ([]InstalledPackItem, error) {
	return s.repo.GetInstalledPacks(ctx, projectID)
}

// AssignPack assigns a template pack to a project
func (s *Service) AssignPack(ctx context.Context, projectID, userID string, req *AssignPackRequest) (*ProjectTemplatePack, error) {
	return s.repo.AssignPack(ctx, projectID, userID, req)
}

// UpdateAssignment updates a pack assignment
func (s *Service) UpdateAssignment(ctx context.Context, projectID, assignmentID string, req *UpdateAssignmentRequest) error {
	return s.repo.UpdateAssignment(ctx, projectID, assignmentID, req)
}

// DeleteAssignment removes a pack assignment from a project
func (s *Service) DeleteAssignment(ctx context.Context, projectID, assignmentID string) error {
	return s.repo.DeleteAssignment(ctx, projectID, assignmentID)
}
