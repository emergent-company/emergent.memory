package orgs

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"go.uber.org/fx"

	"github.com/emergent-company/emergent.memory/pkg/activity"
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
	FindVerifiedUserByEmail(ctx context.Context, email string) (*orgMemberUser, error)
	AddMember(ctx context.Context, orgID, userID, role string) (*OrgMemberDTO, error)
	UpdateMemberRole(ctx context.Context, orgID, userID, role string) (bool, error)
	CountAdmins(ctx context.Context, orgID string) (int, error)
	SoleProjectAdminProjects(ctx context.Context, orgID, userID string) ([]string, error)
	RemoveMemberWithCascade(ctx context.Context, orgID, userID string) ([]string, bool, error)
	CountUserMemberships(ctx context.Context, userID string) (int, error)
	IsUserMember(ctx context.Context, orgID, userID string) (bool, error)
	GetMembershipRole(ctx context.Context, orgID, userID string) (string, error)
	// IsOrgAdmin reports whether the user holds org-admin authority over the
	// org. It delegates to the shared app-side entitlement check
	// (pkg/auth.CanAdministerOrg) so no domain re-derives the role locally
	// (issue #812 §4.5, issue #1162).
	IsOrgAdmin(ctx context.Context, orgID, userID string) (bool, error)
	FindOrgToolSettings(ctx context.Context, orgID string) ([]OrgToolSetting, error)
	UpsertOrgToolSetting(ctx context.Context, setting *OrgToolSetting) (*OrgToolSetting, error)
	DeleteOrgToolSetting(ctx context.Context, orgID, toolName string) (bool, error)
}

// Compile-time check that the concrete repository satisfies the service port.
var _ orgRepository = (*Repository)(nil)

// TokenRevoker revokes a user's project-scoped API tokens. Satisfied by
// apitoken.Repository via fx injection; optional so the domain works without it.
type TokenRevoker interface {
	RevokeByProjectAndUser(ctx context.Context, projectID, userID string) error
}

// Service handles business logic for organizations
type Service struct {
	repo                orgRepository
	log                 *slog.Logger
	toolPoolInvalidator ToolPoolInvalidator
	tokenRevoker        TokenRevoker // optional; nil-safe
}

// ServiceParams bundles dependencies for NewService.
type ServiceParams struct {
	fx.In

	Repo *Repository
	Log  *slog.Logger

	// Optional cross-domain dependencies (nil-safe when not wired).
	ToolPoolInvalidator ToolPoolInvalidator `optional:"true"`
	TokenRevoker        TokenRevoker        `optional:"true"`
}

// NewService creates a new organization service
func NewService(p ServiceParams) *Service {
	return &Service{
		repo:                p.Repo,
		log:                 p.Log.With(logger.Scope("orgs.svc")),
		toolPoolInvalidator: p.ToolPoolInvalidator,
		tokenRevoker:        p.TokenRevoker,
	}
}

// List returns all organizations the user is a member of
func (s *Service) List(ctx context.Context, userID string) ([]OrgDTO, error) {
	if userID == "" {
		return []OrgDTO{}, nil
	}
	return s.repo.List(ctx, userID)
}

// GetByID returns an organization by ID. The caller must be a member of the
// organization; a non-member receives ErrForbidden (fail closed) regardless of
// whether the org exists, so this cannot act as an org-existence oracle.
func (s *Service) GetByID(ctx context.Context, id, userID string) (*OrgDTO, error) {
	if err := s.requireOrgMember(ctx, id, userID); err != nil {
		return nil, err
	}
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

// Update renames an organization and returns the updated DTO. The caller must
// be an org_admin of the addressed organization (renaming is an org-tier write,
// not a membership read); a non-admin receives ErrForbidden before any
// validation or write. Unknown (or soft-deleted) orgs surface the repository's
// not-found error unchanged.
func (s *Service) Update(ctx context.Context, id, userID, name string) (*OrgDTO, error) {
	if err := s.requireOrgAdmin(ctx, id, userID); err != nil {
		return nil, err
	}

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

// Delete deletes an organization by ID. The caller must be an org_admin of the
// addressed organization (deletion is the most destructive org-tier write); a
// non-admin receives ErrForbidden before any write. The bar is org_admin of the
// addressed org, mirroring projects.Transfer: a platform superadmin who is not
// an org_admin of the org is refused here. (A separate platform-level soft-delete
// lives at DELETE /api/superadmin/organizations/:id, gated by superadmin_full —
// this orgs-domain route deliberately stays at the org tier.)
func (s *Service) Delete(ctx context.Context, id, userID string) error {
	if err := s.requireOrgAdmin(ctx, id, userID); err != nil {
		return err
	}

	deleted, err := s.repo.Delete(ctx, id)
	if err != nil {
		return err
	}
	if !deleted {
		return notFoundOrg
	}

	s.log.Info("organization deleted", slog.String("orgID", id))
	return nil
}

// ListMembers returns all members of an organization. Member lists carry user
// PII (emails, names), so the caller must be an org_admin of the addressed
// organization — a plain member cannot enumerate the whole org's identities
// (project-scoped member lists remain the member-level surface). A non-admin
// receives ErrForbidden before any read.
func (s *Service) ListMembers(ctx context.Context, orgID, userID string) ([]OrgMemberDTO, error) {
	if err := s.requireOrgAdmin(ctx, orgID, userID); err != nil {
		return nil, err
	}
	members, err := s.repo.ListMembers(ctx, orgID)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	for i := range members {
		members[i].ActivityTier = activity.TierFor(members[i].LastActiveAt, false, now)
	}
	return members, nil
}

// validOrgRole reports whether role is a supported organization membership role.
func validOrgRole(role string) bool {
	return role == OrgRoleAdmin || role == OrgRoleMember
}

// AddMember directly adds an already-registered user to an organization by
// exact verified email. Organization administration only. The lookup is exact
// (no partial registry browsing); an unregistered email is a 404 that points the
// caller at the invitation flow, and an existing member is a 409.
func (s *Service) AddMember(ctx context.Context, orgID, callerID, email, role string) (*OrgMemberDTO, error) {
	if err := s.requireOrgAdmin(ctx, orgID, callerID); err != nil {
		return nil, err
	}
	role = strings.TrimSpace(role)
	if !validOrgRole(role) {
		return nil, apperror.NewBadRequest("invalid role")
	}
	email = strings.TrimSpace(email)
	if email == "" || !strings.Contains(email, "@") {
		return nil, apperror.NewBadRequest("a valid email address is required")
	}

	found, err := s.repo.FindVerifiedUserByEmail(ctx, email)
	if err != nil {
		return nil, err
	}
	if found == nil {
		return nil, apperror.New(404, "not-found", "No registered user with that email — send an invitation instead.")
	}

	member, err := s.repo.AddMember(ctx, orgID, found.ID, role)
	if err != nil {
		return nil, err
	}

	s.log.Info("organization member added",
		slog.String("orgID", orgID),
		slog.String("userID", found.ID),
		slog.String("role", role),
		slog.String("addedBy", callerID))
	return member, nil
}

// UpdateMemberRole changes an organization member's role in place. The last
// org_admin cannot be demoted, so an organization always retains an
// administrator.
func (s *Service) UpdateMemberRole(ctx context.Context, orgID, callerID, userID, role string) error {
	if err := s.requireOrgAdmin(ctx, orgID, callerID); err != nil {
		return err
	}
	role = strings.TrimSpace(role)
	if !validOrgRole(role) {
		return apperror.NewBadRequest("invalid role")
	}

	current, err := s.repo.GetMembershipRole(ctx, orgID, userID)
	if err != nil {
		return err
	}
	if current == "" {
		return apperror.New(404, "not-found", "Member not found")
	}
	if current == role {
		return nil
	}
	if current == OrgRoleAdmin && role != OrgRoleAdmin {
		admins, err := s.repo.CountAdmins(ctx, orgID)
		if err != nil {
			return err
		}
		if admins <= 1 {
			return apperror.New(403, "last-admin", "Cannot change the last organization admin. Assign another org admin first.")
		}
	}

	updated, err := s.repo.UpdateMemberRole(ctx, orgID, userID, role)
	if err != nil {
		return err
	}
	if !updated {
		return apperror.New(404, "not-found", "Member not found")
	}

	s.log.Info("organization member role changed",
		slog.String("orgID", orgID),
		slog.String("userID", userID),
		slog.String("oldRole", current),
		slog.String("role", role),
		slog.String("changedBy", callerID))
	return nil
}

// RemoveMember removes a member from an organization. Removing an org_admin is
// blocked when they are the last one. Removal cascades to the member's project
// memberships in this organization and revokes their project-scoped tokens, but
// is refused when the member is the only project_admin of a project in the org
// (that project would otherwise be left without an administrator).
func (s *Service) RemoveMember(ctx context.Context, orgID, callerID, userID string) error {
	if err := s.requireOrgAdmin(ctx, orgID, callerID); err != nil {
		return err
	}

	current, err := s.repo.GetMembershipRole(ctx, orgID, userID)
	if err != nil {
		return err
	}
	if current == "" {
		return apperror.New(404, "not-found", "Member not found")
	}
	if current == OrgRoleAdmin {
		admins, err := s.repo.CountAdmins(ctx, orgID)
		if err != nil {
			return err
		}
		if admins <= 1 {
			return apperror.New(403, "last-admin", "Cannot remove the last organization admin. Assign another org admin first.")
		}
	}

	soleAdminOf, err := s.repo.SoleProjectAdminProjects(ctx, orgID, userID)
	if err != nil {
		return err
	}
	if len(soleAdminOf) > 0 {
		return apperror.New(409, "project-without-admin",
			"Cannot remove this member: they are the only project admin of "+strings.Join(soleAdminOf, ", ")+". Assign another project admin first.")
	}

	projectIDs, removed, err := s.repo.RemoveMemberWithCascade(ctx, orgID, userID)
	if err != nil {
		return err
	}
	if !removed {
		return apperror.New(404, "not-found", "Member not found")
	}

	if s.tokenRevoker != nil {
		for _, projectID := range projectIDs {
			if err := s.tokenRevoker.RevokeByProjectAndUser(ctx, projectID, userID); err != nil {
				s.log.Warn("failed to revoke member project tokens",
					slog.String("orgID", orgID),
					slog.String("projectID", projectID),
					slog.String("userID", userID),
					slog.String("error", err.Error()))
			}
		}
	}

	s.log.Info("organization member removed",
		slog.String("orgID", orgID),
		slog.String("userID", userID),
		slog.Int("projectMembershipsRemoved", len(projectIDs)),
		slog.String("removedBy", callerID))
	return nil
}
