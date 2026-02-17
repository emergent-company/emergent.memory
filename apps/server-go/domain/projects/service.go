package projects

import (
	"context"
	"log/slog"
	"regexp"
	"strings"

	"github.com/emergent-company/emergent/pkg/apperror"
	"github.com/emergent-company/emergent/pkg/logger"
)

const (
	// DefaultLimit is the default number of projects to return
	DefaultLimit = 100
	// MaxLimit is the maximum number of projects to return
	MaxLimit = 500
)

var (
	// uuidRegex validates UUID format (36 chars with hyphens)
	uuidRegex = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
)

// Service handles business logic for projects
type Service struct {
	repo *Repository
	log  *slog.Logger
}

// NewService creates a new project service
func NewService(repo *Repository, log *slog.Logger) *Service {
	return &Service{
		repo: repo,
		log:  log.With(logger.Scope("projects.svc")),
	}
}

// ServiceListParams defines parameters for listing projects
type ServiceListParams struct {
	UserID    string
	OrgID     string
	ProjectID string // If set, restrict results to this single project (for API token scope)
	Limit     int
}

// List returns all projects the user is a member of
func (s *Service) List(ctx context.Context, params ServiceListParams) ([]ProjectDTO, error) {
	if params.UserID == "" {
		return []ProjectDTO{}, nil
	}

	// Validate and apply limits
	limit := params.Limit
	if limit <= 0 {
		limit = DefaultLimit
	}
	if limit > MaxLimit {
		limit = MaxLimit
	}

	// Validate orgID if provided - if invalid, return empty list (not an error)
	if params.OrgID != "" && !isValidUUID(params.OrgID) {
		return []ProjectDTO{}, nil
	}

	projects, err := s.repo.List(ctx, ListParams{
		UserID:    params.UserID,
		OrgID:     params.OrgID,
		ProjectID: params.ProjectID,
		Limit:     limit,
	})
	if err != nil {
		return nil, err
	}

	result := make([]ProjectDTO, len(projects))
	for i, p := range projects {
		result[i] = p.ToDTO()
	}
	return result, nil
}

// GetByID returns a project by ID
func (s *Service) GetByID(ctx context.Context, id string) (*ProjectDTO, error) {
	if !isValidUUID(id) {
		return nil, apperror.New(400, "invalid-uuid", "id must be a valid UUID")
	}

	project, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if project == nil {
		return nil, apperror.ErrNotFound.WithMessage("Project not found")
	}

	dto := project.ToDTO()
	return &dto, nil
}

// Create creates a new project
func (s *Service) Create(ctx context.Context, req CreateProjectRequest, userID string) (*ProjectDTO, error) {
	// Validate name
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, apperror.New(400, "validation-failed", "Name required").WithDetails(map[string]any{
			"name": []string{"must not be blank"},
		})
	}

	// Validate orgId is provided
	if req.OrgID == "" {
		return nil, apperror.New(400, "org-required", "Organization id (orgId) is required to create a project")
	}

	// Validate orgId format
	if !isValidUUID(req.OrgID) {
		return nil, apperror.New(400, "invalid-uuid", "orgId must be a valid UUID")
	}

	// Start transaction
	tx, err := s.repo.BeginTx(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	// Check org exists with pessimistic lock
	orgExists, err := s.repo.CheckOrgExistsWithLock(ctx, tx.Tx, req.OrgID)
	if err != nil {
		return nil, err
	}
	if !orgExists {
		return nil, apperror.New(400, "org-not-found", "Organization not found")
	}

	// Check for duplicate name in org
	isDuplicate, err := s.repo.CheckDuplicateName(ctx, tx.Tx, req.OrgID, name, "")
	if err != nil {
		return nil, err
	}
	if isDuplicate {
		return nil, apperror.New(400, "duplicate", "Project with this name exists in org")
	}

	// Create the project
	project := &Project{
		OrganizationID: req.OrgID,
		Name:           name,
	}
	if err := s.repo.Create(ctx, tx.Tx, project); err != nil {
		return nil, err
	}

	// Create membership for the creator as project_admin
	if userID != "" {
		membership := &ProjectMembership{
			ProjectID: project.ID,
			UserID:    userID,
			Role:      RoleProjectAdmin,
		}
		if err := s.repo.CreateMembership(ctx, tx.Tx, membership); err != nil {
			return nil, err
		}
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		s.log.Error("failed to commit transaction", logger.Error(err))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}

	s.log.Info("project created",
		slog.String("projectID", project.ID),
		slog.String("name", project.Name),
		slog.String("orgID", project.OrganizationID),
		slog.String("userID", userID))

	dto := project.ToDTO()
	return &dto, nil
}

// Update updates a project
func (s *Service) Update(ctx context.Context, id string, req UpdateProjectRequest) (*ProjectDTO, error) {
	if !isValidUUID(id) {
		return nil, apperror.New(400, "invalid-uuid", "id must be a valid UUID")
	}

	// Get existing project
	project, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if project == nil {
		return nil, apperror.ErrNotFound.WithMessage("Project not found")
	}

	// Check if there are any updates to apply
	hasUpdates := false

	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			return nil, apperror.New(400, "validation-failed", "Name cannot be empty").WithDetails(map[string]any{
				"name": []string{"must not be blank"},
			})
		}
		if name != project.Name {
			// Check for duplicate name in org
			isDuplicate, err := s.repo.CheckDuplicateName(ctx, nil, project.OrganizationID, name, id)
			if err != nil {
				return nil, err
			}
			if isDuplicate {
				return nil, apperror.New(400, "duplicate", "Project with this name already exists in the organization")
			}
			project.Name = name
			hasUpdates = true
		}
	}

	if req.KBPurpose != nil {
		project.KBPurpose = req.KBPurpose
		hasUpdates = true
	}

	if req.ChatPromptTemplate != nil {
		project.ChatPromptTemplate = req.ChatPromptTemplate
		hasUpdates = true
	}

	if req.AutoExtractObjects != nil {
		project.AutoExtractObjects = *req.AutoExtractObjects
		hasUpdates = true
	}

	if req.AutoExtractConfig != nil {
		project.AutoExtractConfig = req.AutoExtractConfig
		hasUpdates = true
	}

	// If no updates, return current project
	if !hasUpdates {
		dto := project.ToDTO()
		return &dto, nil
	}

	// Update the project
	if err := s.repo.Update(ctx, project); err != nil {
		return nil, err
	}

	s.log.Info("project updated",
		slog.String("projectID", project.ID),
		slog.String("name", project.Name))

	dto := project.ToDTO()
	return &dto, nil
}

// Delete deletes a project
func (s *Service) Delete(ctx context.Context, id string, userID string) error {
	if !isValidUUID(id) {
		return apperror.New(400, "invalid-uuid", "id must be a valid UUID")
	}

	deleted, err := s.repo.Delete(ctx, id)
	if err != nil {
		return err
	}
	if !deleted {
		return apperror.ErrNotFound.WithMessage("Project not found")
	}

	s.log.Info("project deleted",
		slog.String("projectID", id),
		slog.String("deletedBy", userID))

	return nil
}

// ListMembers returns all members of a project
func (s *Service) ListMembers(ctx context.Context, projectID string) ([]ProjectMemberDTO, error) {
	if !isValidUUID(projectID) {
		return nil, apperror.New(400, "invalid-uuid", "projectId must be a valid UUID")
	}

	// Check project exists
	project, err := s.repo.GetByID(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if project == nil {
		return nil, apperror.ErrNotFound.WithMessage("Project not found")
	}

	return s.repo.ListMembers(ctx, projectID)
}

// RemoveMember removes a member from a project
func (s *Service) RemoveMember(ctx context.Context, projectID, userID string) error {
	if !isValidUUID(projectID) {
		return apperror.New(400, "invalid-uuid", "projectId must be a valid UUID")
	}
	if !isValidUUID(userID) {
		return apperror.New(400, "invalid-uuid", "userId must be a valid UUID")
	}

	// Check project exists
	project, err := s.repo.GetByID(ctx, projectID)
	if err != nil {
		return err
	}
	if project == nil {
		return apperror.ErrNotFound.WithMessage("Project not found")
	}

	// Get the membership to check role
	membership, err := s.repo.GetMembership(ctx, projectID, userID)
	if err != nil {
		return err
	}
	if membership == nil {
		return apperror.ErrNotFound.WithMessage("Member not found")
	}

	// If removing an admin, check if they're the last admin
	if membership.Role == RoleProjectAdmin {
		adminCount, err := s.repo.CountAdmins(ctx, projectID)
		if err != nil {
			return err
		}
		if adminCount <= 1 {
			return apperror.New(403, "last-admin", "Cannot remove the last admin from the project. Assign another admin first.")
		}
	}

	// Remove the member
	removed, err := s.repo.RemoveMember(ctx, projectID, userID)
	if err != nil {
		return err
	}
	if !removed {
		return apperror.ErrNotFound.WithMessage("Member not found")
	}

	s.log.Info("project member removed",
		slog.String("projectID", projectID),
		slog.String("userID", userID))

	return nil
}

// IsUserMember checks if a user is a member of a project
func (s *Service) IsUserMember(ctx context.Context, projectID, userID string) (bool, error) {
	return s.repo.IsUserMember(ctx, projectID, userID)
}

// Helper to validate UUID format
func isValidUUID(id string) bool {
	return uuidRegex.MatchString(id)
}
