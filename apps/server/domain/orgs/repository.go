package orgs

import (
	"context"
	"database/sql"
	"log/slog"
	"strings"

	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/internal/database"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
	"github.com/emergent-company/emergent.memory/pkg/logger"
)

// Repository handles database operations for organizations
type Repository struct {
	db  bun.IDB
	log *slog.Logger
}

// NewRepository creates a new organization repository
func NewRepository(db bun.IDB, log *slog.Logger) *Repository {
	return &Repository{
		db:  db,
		log: log.With(logger.Scope("orgs.repo")),
	}
}

// notFoundOrg is the shared organization not-found error, reused by every org
// lookup (GetByID, UpdateName) and the delete service path.
var notFoundOrg = apperror.ErrNotFound.WithMessage("Organization not found")

// dbErr wraps a database failure in the standard database apperror, so the
// apperror chaining is declared once and reused across the repository.
func dbErr(err error) *apperror.Error {
	return apperror.ErrDatabase.WithInternal(err)
}

// List returns all organizations the user is a member of
func (r *Repository) List(ctx context.Context, userID string) ([]OrgDTO, error) {
	var orgs []Org

	err := r.db.NewSelect().
		Model(&orgs).
		Join("INNER JOIN kb.organization_memberships AS om ON om.organization_id = o.id").
		Where("om.user_id = ?", userID).
		Where("o.deleted_at IS NULL").
		Order("o.created_at DESC").
		Scan(ctx)

	if err != nil {
		r.log.Error("failed to list organizations", logger.Error(err))
		return nil, dbErr(err)
	}

	result := make([]OrgDTO, len(orgs))
	for i, org := range orgs {
		result[i] = org.ToDTO()
	}
	return result, nil
}

// GetByID returns an organization by ID
func (r *Repository) GetByID(ctx context.Context, id string) (*Org, error) {
	var org Org

	err := r.db.NewSelect().
		Model(&org).
		Where("id = ?", id).
		Where("deleted_at IS NULL").
		Scan(ctx)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, notFoundOrg
		}
		r.log.Error("failed to get organization", logger.Error(err), slog.String("id", id))
		return nil, dbErr(err)
	}

	return &org, nil
}

// UpdateName renames an organization, bumping updated_at. Soft-deleted rows are
// excluded, so renaming a deleted (or unknown) org reports not found.
func (r *Repository) UpdateName(ctx context.Context, id, name string) (*Org, error) {
	org := &Org{}

	err := r.db.NewUpdate().
		Model(org).
		Set("name = ?", name).
		Set("updated_at = NOW()").
		Where("id = ?", id).
		Where("deleted_at IS NULL").
		Returning("*").
		Scan(ctx)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, notFoundOrg
		}
		r.log.Error("failed to update organization name", logger.Error(err), slog.String("id", id))
		return nil, dbErr(err)
	}

	return org, nil
}

// CountUserMemberships returns the number of organizations a user is a member of
func (r *Repository) CountUserMemberships(ctx context.Context, userID string) (int, error) {
	count, err := r.db.NewSelect().
		Model((*OrganizationMembership)(nil)).
		Where("user_id = ?", userID).
		Count(ctx)

	if err != nil {
		r.log.Error("failed to count user memberships", logger.Error(err), slog.String("userID", userID))
		return 0, apperror.ErrDatabase.WithInternal(err)
	}

	return count, nil
}

// Create creates a new organization and adds the creator as org_admin
func (r *Repository) Create(ctx context.Context, name string, userID string) (*Org, error) {
	tx, err := database.BeginSafeTx(ctx, r.db)
	if err != nil {
		r.log.Error("failed to begin transaction", logger.Error(err))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}
	defer tx.Rollback()

	// Create the organization
	org := &Org{Name: name}
	_, err = tx.NewInsert().
		Model(org).
		Returning("*").
		Exec(ctx)

	if err != nil {
		// Check for unique constraint violation (duplicate name)
		if isUniqueViolation(err) {
			return nil, apperror.New(409, "conflict", "Organization name already exists")
		}
		r.log.Error("failed to create organization", logger.Error(err))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}

	// Create membership for the creator as org_admin
	membership := &OrganizationMembership{
		OrganizationID: org.ID,
		UserID:         userID,
		Role:           "org_admin",
	}
	_, err = tx.NewInsert().
		Model(membership).
		Exec(ctx)

	if err != nil {
		// Check for FK violation (user profile doesn't exist)
		if isForeignKeyViolation(err) {
			return nil, apperror.ErrBadRequest.WithMessage("User profile not properly initialized. Please try logging out and back in.")
		}
		r.log.Error("failed to create membership", logger.Error(err))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}

	if err := tx.Commit(); err != nil {
		r.log.Error("failed to commit transaction", logger.Error(err))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}

	return org, nil
}

// Delete hard deletes an organization by ID and its associated memberships
func (r *Repository) Delete(ctx context.Context, id string) (bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		r.log.Error("failed to begin transaction", logger.Error(err))
		return false, apperror.ErrDatabase.WithInternal(err)
	}
	defer tx.Rollback()

	_, err = tx.NewDelete().
		Model((*OrganizationMembership)(nil)).
		Where("organization_id = ?", id).
		Exec(ctx)
	if err != nil {
		r.log.Error("failed to delete organization memberships", logger.Error(err), slog.String("id", id))
		return false, apperror.ErrDatabase.WithInternal(err)
	}

	result, err := tx.NewDelete().
		Model((*Org)(nil)).
		Where("id = ?", id).
		Exec(ctx)
	if err != nil {
		r.log.Error("failed to delete organization", logger.Error(err), slog.String("id", id))
		return false, apperror.ErrDatabase.WithInternal(err)
	}

	if err := tx.Commit(); err != nil {
		r.log.Error("failed to commit transaction", logger.Error(err))
		return false, apperror.ErrDatabase.WithInternal(err)
	}

	rowsAffected, _ := result.RowsAffected()
	return rowsAffected > 0, nil
}

// IsUserMember checks if a user is a member of an organization
func (r *Repository) IsUserMember(ctx context.Context, orgID, userID string) (bool, error) {
	exists, err := r.db.NewSelect().
		Model((*OrganizationMembership)(nil)).
		Where("organization_id = ?", orgID).
		Where("user_id = ?", userID).
		Exists(ctx)

	if err != nil {
		r.log.Error("failed to check membership", logger.Error(err),
			slog.String("orgID", orgID),
			slog.String("userID", userID))
		return false, apperror.ErrDatabase.WithInternal(err)
	}

	return exists, nil
}

// GetMembershipRole returns the user's role in an organization (e.g. "org_admin"
// or "member"), or an empty string if the user is not a member.
func (r *Repository) GetMembershipRole(ctx context.Context, orgID, userID string) (string, error) {
	var role string

	err := r.db.NewSelect().
		TableExpr("kb.organization_memberships AS om").
		ColumnExpr("om.role").
		Where("om.organization_id = ?", orgID).
		Where("om.user_id = ?", userID).
		Scan(ctx, &role)

	if err != nil {
		if err == sql.ErrNoRows {
			return "", nil
		}
		r.log.Error("failed to get membership role", logger.Error(err),
			slog.String("orgID", orgID),
			slog.String("userID", userID))
		return "", apperror.ErrDatabase.WithInternal(err)
	}

	return role, nil
}

// IsOrgAdmin reports whether the user holds org-admin authority over the
// organization. It delegates to the single app-side org-administration decision
// (pkg/auth.CanAdministerOrg) so no org-scoped decision point re-derives the
// role locally (issue #812 §4.5, issue #1162). Fail closed on a lookup error.
func (r *Repository) IsOrgAdmin(ctx context.Context, orgID, userID string) (bool, error) {
	ok, err := auth.CanAdministerOrg(ctx, r.db, orgID, userID)
	if err != nil {
		r.log.Error("failed to check org_admin authority", logger.Error(err),
			slog.String("orgID", orgID),
			slog.String("userID", userID))
		return false, dbErr(err)
	}
	return ok, nil
}

// ListMembers returns all members of an organization with their user profile info.
func (r *Repository) ListMembers(ctx context.Context, orgID string) ([]OrgMemberDTO, error) {
	var members []OrgMemberDTO

	err := r.db.NewSelect().
		TableExpr("kb.organization_memberships AS om").
		ColumnExpr("up.id").
		ColumnExpr("COALESCE(ue.email, '') AS email").
		ColumnExpr("up.display_name").
		ColumnExpr("up.first_name").
		ColumnExpr("up.last_name").
		ColumnExpr("om.role").
		ColumnExpr("om.created_at AS joined_at").
		ColumnExpr(`(
			SELECT MAX(a.last_seen_at)
			FROM kb.user_activity_daily AS a
			WHERE a.user_id = om.user_id
			  AND a.organization_id = om.organization_id
		) AS last_active_at`).
		Join("INNER JOIN core.user_profiles AS up ON up.id = om.user_id").
		Join(`LEFT JOIN LATERAL (
			SELECT email FROM core.user_emails
			WHERE user_id = up.id
			ORDER BY verified DESC, created_at ASC
			LIMIT 1
		) AS ue ON true`).
		Where("om.organization_id = ?", orgID).
		Order("om.created_at ASC").
		Scan(ctx, &members)

	if err != nil {
		r.log.Error("failed to list org members", logger.Error(err), slog.String("orgID", orgID))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}

	return members, nil
}

// orgMemberUser is the identity surface resolved for a direct-add target.
type orgMemberUser struct {
	ID          string
	Email       string
	DisplayName *string
	FirstName   *string
	LastName    *string
}

// FindVerifiedUserByEmail resolves a registered user by exact (case-insensitive)
// verified email. Return nil, nil when no such user exists. Deliberately exact
// match only: org admins may add a known registered person, but cannot browse
// the user registry.
func (r *Repository) FindVerifiedUserByEmail(ctx context.Context, email string) (*orgMemberUser, error) {
	email = strings.TrimSpace(strings.ToLower(email))
	if email == "" {
		return nil, nil
	}

	var u orgMemberUser
	err := r.db.NewSelect().
		TableExpr("core.user_emails AS ue").
		ColumnExpr("up.id").
		ColumnExpr("ue.email").
		ColumnExpr("up.display_name").
		ColumnExpr("up.first_name").
		ColumnExpr("up.last_name").
		Join("INNER JOIN core.user_profiles AS up ON up.id = ue.user_id").
		Where("lower(ue.email) = ?", email).
		Where("ue.verified = true").
		Where("up.deleted_at IS NULL").
		Limit(1).
		Scan(ctx, &u)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		r.log.Error("failed to find user by email", logger.Error(err))
		return nil, dbErr(err)
	}
	return &u, nil
}

// AddMember inserts an organization membership and returns the resulting member
// row (identity + role + joined_at), matching ListMembers' shape.
func (r *Repository) AddMember(ctx context.Context, orgID, userID, role string) (*OrgMemberDTO, error) {
	membership := &OrganizationMembership{
		OrganizationID: orgID,
		UserID:         userID,
		Role:           role,
	}
	if _, err := r.db.NewInsert().Model(membership).Exec(ctx); err != nil {
		if isUniqueViolation(err) {
			return nil, apperror.NewConflict("already a member of this organization")
		}
		if isForeignKeyViolation(err) {
			return nil, apperror.NewBadRequest("user profile not initialized")
		}
		r.log.Error("failed to add organization member", logger.Error(err),
			slog.String("orgID", orgID), slog.String("userID", userID))
		return nil, dbErr(err)
	}

	member, err := r.getMember(ctx, orgID, userID)
	if err != nil {
		return nil, err
	}
	if member == nil {
		return nil, apperror.NewNotFound("member", userID)
	}
	return member, nil
}

// getMember returns one org member row, or nil when the membership is absent.
func (r *Repository) getMember(ctx context.Context, orgID, userID string) (*OrgMemberDTO, error) {
	var member OrgMemberDTO
	err := r.db.NewSelect().
		TableExpr("kb.organization_memberships AS om").
		ColumnExpr("up.id").
		ColumnExpr("COALESCE(ue.email, '') AS email").
		ColumnExpr("up.display_name").
		ColumnExpr("up.first_name").
		ColumnExpr("up.last_name").
		ColumnExpr("om.role").
		ColumnExpr("om.created_at AS joined_at").
		Join("INNER JOIN core.user_profiles AS up ON up.id = om.user_id").
		Join(`LEFT JOIN LATERAL (
			SELECT email FROM core.user_emails
			WHERE user_id = up.id
			ORDER BY verified DESC, created_at ASC
			LIMIT 1
		) AS ue ON true`).
		Where("om.organization_id = ?", orgID).
		Where("om.user_id = ?", userID).
		Limit(1).
		Scan(ctx, &member)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, dbErr(err)
	}
	return &member, nil
}

// UpdateMemberRole changes one organization membership's role in place.
// Returns false when the membership does not exist.
func (r *Repository) UpdateMemberRole(ctx context.Context, orgID, userID, role string) (bool, error) {
	result, err := r.db.NewUpdate().
		Model((*OrganizationMembership)(nil)).
		Set("role = ?", role).
		Where("organization_id = ?", orgID).
		Where("user_id = ?", userID).
		Exec(ctx)
	if err != nil {
		r.log.Error("failed to update organization member role", logger.Error(err),
			slog.String("orgID", orgID), slog.String("userID", userID))
		return false, dbErr(err)
	}
	rows, _ := result.RowsAffected()
	return rows > 0, nil
}

// CountAdmins counts org_admin memberships in an organization.
func (r *Repository) CountAdmins(ctx context.Context, orgID string) (int, error) {
	count, err := r.db.NewSelect().
		Model((*OrganizationMembership)(nil)).
		Where("organization_id = ?", orgID).
		Where("role = ?", OrgRoleAdmin).
		Count(ctx)
	if err != nil {
		r.log.Error("failed to count organization admins", logger.Error(err), slog.String("orgID", orgID))
		return 0, dbErr(err)
	}
	return count, nil
}

// SoleProjectAdminProjects returns the names of active projects in the org where
// the user is the only project_admin. A cascade org-member removal must refuse
// rather than leave such a project adminless.
func (r *Repository) SoleProjectAdminProjects(ctx context.Context, orgID, userID string) ([]string, error) {
	var names []string
	err := r.db.NewRaw(`
		SELECT p.name
		FROM kb.projects p
		JOIN kb.project_memberships pm ON pm.project_id = p.id
		WHERE p.organization_id = ?
		  AND p.deleted_at IS NULL
		  AND pm.user_id = ?
		  AND pm.role = 'project_admin'
		  AND (
			SELECT COUNT(*) FROM kb.project_memberships pm2
			WHERE pm2.project_id = p.id AND pm2.role = 'project_admin'
		  ) = 1
		ORDER BY p.name ASC
	`, orgID, userID).Scan(ctx, &names)
	if err != nil {
		r.log.Error("failed to check sole project admins", logger.Error(err),
			slog.String("orgID", orgID), slog.String("userID", userID))
		return nil, dbErr(err)
	}
	return names, nil
}

// RemoveMemberWithCascade deletes the org membership and every project
// membership the user holds in that org's projects, atomically. It returns the
// affected project ids so the caller can revoke their project-scoped tokens.
func (r *Repository) RemoveMemberWithCascade(ctx context.Context, orgID, userID string) ([]string, bool, error) {
	tx, err := database.BeginSafeTx(ctx, r.db)
	if err != nil {
		r.log.Error("failed to begin transaction", logger.Error(err))
		return nil, false, dbErr(err)
	}
	defer func() { _ = tx.Rollback() }()

	var projectIDs []string
	if err := tx.NewSelect().
		TableExpr("kb.project_memberships AS pm").
		ColumnExpr("pm.project_id").
		Join("INNER JOIN kb.projects AS p ON p.id = pm.project_id").
		Where("p.organization_id = ?", orgID).
		Where("pm.user_id = ?", userID).
		Scan(ctx, &projectIDs); err != nil && err != sql.ErrNoRows {
		return nil, false, dbErr(err)
	}

	if len(projectIDs) > 0 {
		if _, err := tx.NewDelete().
			TableExpr("kb.project_memberships").
			Where("user_id = ?", userID).
			Where("project_id IN (SELECT id FROM kb.projects WHERE organization_id = ?)", orgID).
			Exec(ctx); err != nil {
			r.log.Error("failed to cascade project memberships", logger.Error(err),
				slog.String("orgID", orgID), slog.String("userID", userID))
			return nil, false, dbErr(err)
		}
	}

	result, err := tx.NewDelete().
		Model((*OrganizationMembership)(nil)).
		Where("organization_id = ?", orgID).
		Where("user_id = ?", userID).
		Exec(ctx)
	if err != nil {
		r.log.Error("failed to remove organization member", logger.Error(err),
			slog.String("orgID", orgID), slog.String("userID", userID))
		return nil, false, dbErr(err)
	}

	if err := tx.Commit(); err != nil {
		r.log.Error("failed to commit member removal", logger.Error(err))
		return nil, false, dbErr(err)
	}

	rows, _ := result.RowsAffected()
	return projectIDs, rows > 0, nil
}

// Helper functions to check PostgreSQL error codes
func isUniqueViolation(err error) bool {
	return containsErrorCode(err, "23505")
}

func isForeignKeyViolation(err error) bool {
	return containsErrorCode(err, "23503")
}

func containsErrorCode(err error, code string) bool {
	if err == nil {
		return false
	}
	// pgx wraps errors, so we need to check the error message
	errStr := err.Error()
	return len(errStr) > 0 && (contains(errStr, code) || contains(errStr, "SQLSTATE "+code))
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchSubstring(s, substr)
}

func searchSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
