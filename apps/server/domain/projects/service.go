package projects

import (
	"context"
	"errors"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"go.uber.org/fx"

	"github.com/emergent-company/emergent.memory/domain/agents"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/logger"
)

const (
	// DefaultLimit of 0 means no limit — return all projects
	DefaultLimit = 0
	// MaxLimit caps explicit limit requests
	MaxLimit = 1000
	// DefaultDeletionGracePeriod is the default window between marking a project
	// pending deletion and hard-purging it. Callers can restore within this window.
	DefaultDeletionGracePeriod = time.Hour
)

var (
	// uuidRegex validates UUID format (36 chars with hyphens)
	uuidRegex = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
)

// TokenRevoker can revoke API tokens for a user scoped to a project.
// Satisfied by apitoken.Repository via fx injection.
type TokenRevoker interface {
	RevokeByProjectAndUser(ctx context.Context, projectID, userID string) error
}

// BranchReader can look up a project's main branch.
// Satisfied by branches.Store via fx injection.
type BranchReader interface {
	GetMainBranchID(ctx context.Context, projectID string) (*string, error)
}

// OrgMembershipReader returns a user's role in an organization.
// Satisfied by orgs.Repository via fx injection.
type OrgMembershipReader interface {
	GetMembershipRole(ctx context.Context, orgID, userID string) (string, error)
}

// deletionRepository abstracts the persistence operations used by the project
// deletion lifecycle (mark, inspect, cancel). *Repository satisfies it.
// Keeping it an interface allows the lifecycle to be unit-tested without a DB.
type deletionRepository interface {
	GetDeletionState(ctx context.Context, id string) (scheduledFor *time.Time, deletedAt *time.Time, found bool, err error)
	MarkPendingDeletion(ctx context.Context, id string, userID string, scheduleAt time.Time) (bool, error)
	CancelPendingDeletion(ctx context.Context, id string) (bool, error)
}

// Service handles business logic for projects
type Service struct {
	repo                *Repository
	agentRepo           *agents.Repository
	tokenRevoker        TokenRevoker        // optional; nil is safe
	branchReader        BranchReader        // optional; nil is safe
	orgMembershipReader OrgMembershipReader // optional; nil is safe
	deletionRepo        deletionRepository  // optional; nil is safe
	gracePeriod         time.Duration
	log                 *slog.Logger
}

// ServiceParams bundles dependencies for NewService.
type ServiceParams struct {
	fx.In

	Repo      *Repository
	AgentRepo *agents.Repository
	Log       *slog.Logger

	// Optional cross-domain dependencies (nil-safe when not wired).
	TokenRevoker        TokenRevoker        `optional:"true"`
	BranchReader        BranchReader        `optional:"true"`
	OrgMembershipReader OrgMembershipReader `optional:"true"`
}

// NewService creates a new project service
func NewService(p ServiceParams) *Service {
	return &Service{
		repo:                p.Repo,
		agentRepo:           p.AgentRepo,
		tokenRevoker:        p.TokenRevoker,
		branchReader:        p.BranchReader,
		orgMembershipReader: p.OrgMembershipReader,
		deletionRepo:        p.Repo,
		gracePeriod:         DefaultDeletionGracePeriod,
		log:                 p.Log.With(logger.Scope("projects.svc")),
	}
}

// ConfigureDeletionGracePeriod overrides the window between marking a project
// pending deletion and hard-purging it. Non-positive values are ignored.
func (s *Service) ConfigureDeletionGracePeriod(d time.Duration) {
	if d <= 0 {
		return
	}
	s.gracePeriod = d
}

// ServiceListParams defines parameters for listing projects
type ServiceListParams struct {
	UserID         string
	OrgID          string
	ProjectID      string // If set, restrict results to this single project (for API token scope)
	IncludeStats   bool   // Whether to include aggregate statistics
	IncludePending bool   // Whether to include projects pending deletion
	Limit          int
}

// enrichWithMainBranch populates dto.MainBranchID from the branch store (best-effort; non-fatal).
func (s *Service) enrichWithMainBranch(ctx context.Context, dto *ProjectDTO) {
	if s.branchReader == nil {
		return
	}
	id, err := s.branchReader.GetMainBranchID(ctx, dto.ID)
	if err != nil {
		s.log.WarnContext(ctx, "failed to fetch main branch id", "project_id", dto.ID, "err", err)
		return
	}
	dto.MainBranchID = id
}

// List returns all projects the user is a member of
func (s *Service) List(ctx context.Context, params ServiceListParams) ([]ProjectDTO, error) {
	if params.UserID == "" {
		return []ProjectDTO{}, nil
	}

	// Validate and apply limits; 0 means no limit (return all)
	limit := params.Limit
	if limit < 0 {
		limit = 0
	}
	if limit > MaxLimit {
		limit = MaxLimit
	}

	// Validate orgID if provided - if invalid, return empty list (not an error)
	if params.OrgID != "" && !isValidUUID(params.OrgID) {
		return []ProjectDTO{}, nil
	}

	projects, err := s.repo.List(ctx, ListParams{
		UserID:         params.UserID,
		OrgID:          params.OrgID,
		ProjectID:      params.ProjectID,
		IncludeStats:   params.IncludeStats,
		IncludePending: params.IncludePending,
		Limit:          limit,
	})
	if err != nil {
		return nil, err
	}

	result := make([]ProjectDTO, len(projects))
	for i, p := range projects {
		dto := p.ToDTO()
		s.enrichWithMainBranch(ctx, &dto)
		result[i] = dto
	}
	return result, nil
}

// GetByID returns a project by ID
func (s *Service) GetByID(ctx context.Context, id string, includeStats bool) (*ProjectDTO, error) {
	if !isValidUUID(id) {
		return nil, apperror.New(400, "invalid-uuid", "id must be a valid UUID")
	}

	project, err := s.repo.GetByID(ctx, id, includeStats)
	if err != nil {
		return nil, err
	}
	if project == nil {
		return nil, apperror.ErrNotFound.WithMessage("Project not found")
	}

	dto := project.ToDTO()
	s.enrichWithMainBranch(ctx, &dto)
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

	// Create the project — default monthly budget of $10 USD
	defaultBudget := 10.0
	project := &Project{
		OrganizationID: req.OrgID,
		Name:           name,
		BudgetUSD:      &defaultBudget,
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

	// Eagerly provision the graph-query-agent for every new project so it is
	// ready immediately without a separate install step.
	if _, err := s.agentRepo.EnsureGraphQueryAgent(ctx, project.ID); err != nil {
		// Non-fatal: the agent will be created lazily on first /query call.
		s.log.Warn("failed to provision graph-query-agent for new project",
			slog.String("projectID", project.ID),
			slog.String("error", err.Error()))
	}

	dto := project.ToDTO()
	s.enrichWithMainBranch(ctx, &dto)
	return &dto, nil
}

// Update updates a project
func (s *Service) Update(ctx context.Context, id string, req UpdateProjectRequest) (*ProjectDTO, error) {
	if !isValidUUID(id) {
		return nil, apperror.New(400, "invalid-uuid", "id must be a valid UUID")
	}

	// Get existing project
	project, err := s.repo.GetByID(ctx, id, false)
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

	if req.ProjectInfo != nil {
		project.ProjectInfo = req.ProjectInfo
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

	if req.AutoMergeExtractionBranches != nil {
		project.AutoMergeExtractionBranches = *req.AutoMergeExtractionBranches
		hasUpdates = true
	}

	if req.AutoExtractConfig != nil {
		project.AutoExtractConfig = req.AutoExtractConfig
		hasUpdates = true
	}

	if req.BudgetUSD != nil {
		project.BudgetUSD = req.BudgetUSD
		hasUpdates = true
	}

	if req.BudgetAlertThreshold != nil {
		project.BudgetAlertThreshold = *req.BudgetAlertThreshold
		hasUpdates = true
	}

	// If no updates, return current project
	if !hasUpdates {
		dto := project.ToDTO()
		s.enrichWithMainBranch(ctx, &dto)
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
	s.enrichWithMainBranch(ctx, &dto)
	return &dto, nil
}

// Transfer reparents a project to another organization.
func (s *Service) Transfer(ctx context.Context, projectID, destOrgID, userID string) (*ProjectDTO, error) {
	if !isValidUUID(projectID) {
		return nil, apperror.New(400, "invalid-uuid", "id must be a valid UUID")
	}
	if !isValidUUID(destOrgID) {
		return nil, apperror.New(400, "invalid-uuid", "orgId must be a valid UUID")
	}

	// Get existing project
	project, err := s.repo.GetByID(ctx, projectID, false)
	if err != nil {
		return nil, err
	}
	if project == nil {
		return nil, apperror.ErrNotFound.WithMessage("Project not found")
	}

	sourceOrg := project.OrganizationID
	if sourceOrg == destOrgID {
		return nil, apperror.New(400, "bad_request", "Project already belongs to the destination organization")
	}

	if s.orgMembershipReader == nil {
		return nil, apperror.ErrInternal.WithInternal(errors.New("org membership reader not configured"))
	}

	// Requester must be org_admin of the project's current org.
	role, err := s.orgMembershipReader.GetMembershipRole(ctx, sourceOrg, userID)
	if err != nil {
		return nil, err
	}
	if role != "org_admin" {
		return nil, apperror.ErrForbidden.WithMessage("Only an org_admin of the project's current organization can transfer it")
	}

	// Requester must be a member of the destination org.
	role, err = s.orgMembershipReader.GetMembershipRole(ctx, destOrgID, userID)
	if err != nil {
		return nil, err
	}
	if role == "" {
		return nil, apperror.ErrForbidden.WithMessage("You must be a member of the destination organization")
	}

	if err := s.repo.TransferProject(ctx, projectID, destOrgID); err != nil {
		return nil, err
	}

	project.OrganizationID = destOrgID

	s.log.Info("project transferred",
		slog.String("projectID", project.ID),
		slog.String("fromOrg", sourceOrg),
		slog.String("toOrg", destOrgID),
		slog.String("userID", userID))

	dto := project.ToDTO()
	s.enrichWithMainBranch(ctx, &dto)
	return &dto, nil
}

// DeletionInfo describes the outcome of a project deletion request.
type DeletionInfo struct {
	// ScheduledFor is the time at which the project will be hard-purged.
	ScheduledFor time.Time
	// AlreadyPending is true when the project was already pending deletion and
	// no new schedule was created (idempotent repeat request).
	AlreadyPending bool
}

// RequestDeletion synchronously marks a project as pending deletion and schedules
// it for hard purge after the configured grace period. It is idempotent: if the
// project is already pending deletion the existing schedule is returned with
// AlreadyPending=true and no error.
func (s *Service) RequestDeletion(ctx context.Context, id string, userID string) (*DeletionInfo, error) {
	if !isValidUUID(id) {
		return nil, apperror.New(400, "invalid-uuid", "id must be a valid UUID")
	}
	if s.deletionRepo == nil {
		return nil, apperror.NewDatabase("Deletion repository not configured", nil)
	}

	scheduledFor, _, found, err := s.deletionRepo.GetDeletionState(ctx, id)
	if err != nil {
		return nil, err
	}
	if found && scheduledFor != nil {
		return &DeletionInfo{ScheduledFor: *scheduledFor, AlreadyPending: true}, nil
	}

	grace := s.gracePeriod
	if grace <= 0 {
		grace = DefaultDeletionGracePeriod
	}
	scheduleAt := time.Now().Add(grace)

	marked, err := s.deletionRepo.MarkPendingDeletion(ctx, id, userID, scheduleAt)
	if err != nil {
		return nil, err
	}
	if !marked {
		// Another request may have marked it between our read and update, or the
		// project does not exist. Re-read to distinguish those cases.
		scheduledFor, _, found, rerr := s.deletionRepo.GetDeletionState(ctx, id)
		if rerr != nil {
			return nil, rerr
		}
		if found && scheduledFor != nil {
			return &DeletionInfo{ScheduledFor: *scheduledFor, AlreadyPending: true}, nil
		}
		return nil, apperror.ErrNotFound.WithMessage("Project not found")
	}

	s.log.Info("project marked pending deletion",
		slog.String("projectID", id),
		slog.String("initiatedBy", userID),
		slog.Time("scheduledFor", scheduleAt))

	return &DeletionInfo{ScheduledFor: scheduleAt}, nil
}

// CancelDeletion restores a project that is within its deletion grace period.
func (s *Service) CancelDeletion(ctx context.Context, id string) error {
	if !isValidUUID(id) {
		return apperror.New(400, "invalid-uuid", "id must be a valid UUID")
	}
	if s.deletionRepo == nil {
		return apperror.NewDatabase("Deletion repository not configured", nil)
	}

	cancelled, err := s.deletionRepo.CancelPendingDeletion(ctx, id)
	if err != nil {
		return err
	}
	if !cancelled {
		return apperror.ErrNotFound.WithMessage("Project not pending deletion")
	}

	s.log.Info("project deletion cancelled, project restored", slog.String("projectID", id))
	return nil
}

// ListMembers returns all members of a project.
// When includeStats is true, each member includes lastActiveAt from their API tokens.
func (s *Service) ListMembers(ctx context.Context, projectID string, includeStats bool) ([]ProjectMemberDTO, error) {
	if !isValidUUID(projectID) {
		return nil, apperror.New(400, "invalid-uuid", "projectId must be a valid UUID")
	}

	// Check project exists
	project, err := s.repo.GetByID(ctx, projectID, false)
	if err != nil {
		return nil, err
	}
	if project == nil {
		return nil, apperror.ErrNotFound.WithMessage("Project not found")
	}

	return s.repo.ListMembers(ctx, projectID, includeStats)
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
	project, err := s.repo.GetByID(ctx, projectID, false)
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

	// Revoke all project-scoped tokens for the removed member
	if s.tokenRevoker != nil {
		if revokeErr := s.tokenRevoker.RevokeByProjectAndUser(ctx, projectID, userID); revokeErr != nil {
			// Non-fatal: log and continue — membership is already removed
			s.log.Warn("failed to revoke member tokens on removal",
				slog.String("projectID", projectID),
				slog.String("userID", userID),
				slog.String("error", revokeErr.Error()))
		}
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
