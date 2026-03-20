package schemas

import (
	"context"
	"log/slog"

	"github.com/emergent-company/emergent.memory/pkg/logger"
)

// Service handles business logic for memory schemas
type Service struct {
	repo *Repository
	log  *slog.Logger
}

// NewService creates a new schemas service
func NewService(repo *Repository, log *slog.Logger) *Service {
	return &Service{
		repo: repo,
		log:  log.With(logger.Scope("schemas.svc")),
	}
}

// GetCompiledTypes returns compiled object and relationship types for a project
func (s *Service) GetCompiledTypes(ctx context.Context, projectID string) (*CompiledTypesResponse, error) {
	return s.repo.GetCompiledTypesByProject(ctx, projectID)
}

// GetAvailablePacks returns schemas available for a project to install.
// Returns schemas owned by the project, plus org-visible schemas from the same org.
func (s *Service) GetAvailablePacks(ctx context.Context, projectID, orgID string) ([]MemorySchemaListItem, error) {
	return s.repo.GetAvailablePacks(ctx, projectID, orgID)
}

// GetInstalledPacks returns schemas installed for a project
func (s *Service) GetInstalledPacks(ctx context.Context, projectID string) ([]InstalledSchemaItem, error) {
	return s.repo.GetInstalledPacks(ctx, projectID)
}

// AssignPack assigns a schema to a project and registers its types.
// When req.DryRun is true, returns a preview without making any changes.
// When req.Merge is true, additively merges incoming schemas into existing types.
func (s *Service) AssignPack(ctx context.Context, projectID, userID string, req *AssignPackRequest) (*AssignPackResult, error) {
	return s.repo.AssignPackWithTypes(ctx, projectID, userID, req)
}

// UpdateAssignment updates a pack assignment
func (s *Service) UpdateAssignment(ctx context.Context, projectID, assignmentID string, req *UpdateAssignmentRequest) error {
	return s.repo.UpdateAssignment(ctx, projectID, assignmentID, req)
}

// DeleteAssignment removes a pack assignment from a project
func (s *Service) DeleteAssignment(ctx context.Context, projectID, assignmentID string) error {
	return s.repo.DeleteAssignment(ctx, projectID, assignmentID)
}

// CreatePack creates a new schema scoped to the given project and org
func (s *Service) CreatePack(ctx context.Context, projectID, orgID string, req *CreatePackRequest) (*GraphMemorySchema, error) {
	return s.repo.CreatePack(ctx, projectID, orgID, req)
}

// GetPack returns a schema by ID if the caller has access (same project or same org with org visibility)
func (s *Service) GetPack(ctx context.Context, packID, projectID, orgID string) (*GraphMemorySchema, error) {
	return s.repo.GetPack(ctx, packID, projectID, orgID)
}

// UpdatePack partially updates an existing schema the caller owns
func (s *Service) UpdatePack(ctx context.Context, packID, projectID, orgID string, req *UpdatePackRequest) (*GraphMemorySchema, error) {
	return s.repo.UpdatePack(ctx, packID, projectID, orgID, req)
}

// DeletePack deletes a schema the caller owns from the registry
func (s *Service) DeletePack(ctx context.Context, packID, projectID, orgID string) error {
	return s.repo.DeletePack(ctx, packID, projectID, orgID)
}
