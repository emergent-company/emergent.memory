package projects

import (
	"context"
	"database/sql"
	"log/slog"
	"strings"

	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent/internal/database"
	"github.com/emergent-company/emergent/pkg/apperror"
	"github.com/emergent-company/emergent/pkg/logger"
)

// Repository handles database operations for projects
type Repository struct {
	db  bun.IDB
	log *slog.Logger
}

// NewRepository creates a new project repository
func NewRepository(db bun.IDB, log *slog.Logger) *Repository {
	return &Repository{
		db:  db,
		log: log.With(logger.Scope("projects.repo")),
	}
}

// ListParams defines parameters for listing projects
type ListParams struct {
	UserID    string
	OrgID     string // Optional filter by org
	ProjectID string // Optional filter by specific project (for API token scope)
	Limit     int
}

// List returns all projects the user is a member of
func (r *Repository) List(ctx context.Context, params ListParams) ([]Project, error) {
	var projects []Project

	query := r.db.NewSelect().
		Model(&projects).
		Join("INNER JOIN kb.project_memberships AS pm ON pm.project_id = p.id").
		Where("pm.user_id = ?", params.UserID).
		Order("p.created_at DESC")

	if params.OrgID != "" {
		query = query.Where("p.organization_id = ?", params.OrgID)
	}

	if params.ProjectID != "" {
		query = query.Where("p.id = ?", params.ProjectID)
	}

	if params.Limit > 0 {
		query = query.Limit(params.Limit)
	}

	err := query.Scan(ctx)
	if err != nil {
		r.log.Error("failed to list projects", logger.Error(err))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}

	return projects, nil
}

// GetByID returns a project by ID
func (r *Repository) GetByID(ctx context.Context, id string) (*Project, error) {
	var project Project

	err := r.db.NewSelect().
		Model(&project).
		Where("id = ?", id).
		Scan(ctx)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil // Return nil, nil for not found (let service decide error)
		}
		r.log.Error("failed to get project", logger.Error(err), slog.String("id", id))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}

	return &project, nil
}

// GetByIDWithLock returns a project by ID with a pessimistic lock (FOR UPDATE)
func (r *Repository) GetByIDWithLock(ctx context.Context, tx bun.Tx, id string) (*Project, error) {
	var project Project

	err := tx.NewSelect().
		Model(&project).
		Where("id = ?", id).
		For("UPDATE").
		Scan(ctx)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		r.log.Error("failed to get project with lock", logger.Error(err), slog.String("id", id))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}

	return &project, nil
}

// CheckOrgExistsWithLock checks if an org exists and locks the row
func (r *Repository) CheckOrgExistsWithLock(ctx context.Context, tx bun.Tx, orgID string) (bool, error) {
	exists, err := tx.NewSelect().
		TableExpr("kb.orgs").
		Where("id = ?", orgID).
		For("UPDATE").
		Exists(ctx)

	if err != nil {
		r.log.Error("failed to check org existence", logger.Error(err), slog.String("orgID", orgID))
		return false, apperror.ErrDatabase.WithInternal(err)
	}

	return exists, nil
}

// CheckDuplicateName checks if a project with the same name exists in the org
// If db is nil, uses the repository's default database connection
func (r *Repository) CheckDuplicateName(ctx context.Context, db bun.IDB, orgID, name string, excludeID string) (bool, error) {
	if db == nil {
		db = r.db
	}
	query := db.NewSelect().
		Model((*Project)(nil)).
		Where("organization_id = ?", orgID).
		Where("LOWER(name) = LOWER(?)", strings.TrimSpace(name))

	if excludeID != "" {
		query = query.Where("id != ?", excludeID)
	}

	exists, err := query.Exists(ctx)
	if err != nil {
		r.log.Error("failed to check duplicate name", logger.Error(err))
		return false, apperror.ErrDatabase.WithInternal(err)
	}

	return exists, nil
}

// Create creates a new project in the database (within a transaction)
func (r *Repository) Create(ctx context.Context, tx bun.Tx, project *Project) error {
	_, err := tx.NewInsert().
		Model(project).
		Returning("id, organization_id, name, kb_purpose, chat_prompt_template, auto_extract_objects, auto_extract_config, created_at, updated_at").
		Exec(ctx)

	if err != nil {
		if isForeignKeyViolation(err) {
			return apperror.New(400, "org-not-found", "Organization not found")
		}
		if isUniqueViolation(err) {
			return apperror.New(400, "duplicate", "Project with this name already exists in the organization")
		}
		r.log.Error("failed to create project", logger.Error(err))
		return apperror.ErrDatabase.WithInternal(err)
	}

	return nil
}

// CreateMembership creates a project membership
func (r *Repository) CreateMembership(ctx context.Context, tx bun.Tx, membership *ProjectMembership) error {
	_, err := tx.NewInsert().
		Model(membership).
		On("CONFLICT (project_id, user_id) DO NOTHING"). // Ignore duplicate memberships
		Exec(ctx)

	if err != nil {
		r.log.Error("failed to create project membership", logger.Error(err))
		return apperror.ErrDatabase.WithInternal(err)
	}

	return nil
}

// Update updates a project in the database
func (r *Repository) Update(ctx context.Context, project *Project) error {
	_, err := r.db.NewUpdate().
		Model(project).
		WherePK().
		Returning("id, organization_id, name, kb_purpose, chat_prompt_template, auto_extract_objects, auto_extract_config, created_at, updated_at").
		Exec(ctx)

	if err != nil {
		if isUniqueViolation(err) {
			return apperror.New(400, "duplicate", "Project with this name already exists in the organization")
		}
		r.log.Error("failed to update project", logger.Error(err), slog.String("id", project.ID))
		return apperror.ErrDatabase.WithInternal(err)
	}

	return nil
}

// Delete permanently deletes a project
// Note: Using hard delete since soft delete columns (deleted_at, deleted_by)
// are added in a later migration (1765826000000-AddSoftDeleteColumns)
func (r *Repository) Delete(ctx context.Context, id string) (bool, error) {
	result, err := r.db.NewDelete().
		Model((*Project)(nil)).
		Where("id = ?", id).
		Exec(ctx)

	if err != nil {
		r.log.Error("failed to delete project", logger.Error(err), slog.String("id", id))
		return false, apperror.ErrDatabase.WithInternal(err)
	}

	rowsAffected, _ := result.RowsAffected()
	return rowsAffected > 0, nil
}

// ListMembers returns all members of a project with their user profile info
func (r *Repository) ListMembers(ctx context.Context, projectID string) ([]ProjectMemberDTO, error) {
	var members []ProjectMemberDTO

	// Note: user_emails table doesn't have is_primary column in base schema
	// Using DISTINCT ON to get one email per user (prioritizing verified emails)
	err := r.db.NewSelect().
		TableExpr("kb.project_memberships AS pm").
		ColumnExpr("up.id").
		ColumnExpr("COALESCE(ue.email, '') AS email").
		ColumnExpr("up.display_name").
		ColumnExpr("up.first_name").
		ColumnExpr("up.last_name").
		ColumnExpr("up.avatar_object_key AS avatar_url").
		ColumnExpr("pm.role").
		ColumnExpr("pm.created_at AS joined_at").
		Join("INNER JOIN core.user_profiles AS up ON up.id = pm.user_id").
		Join(`LEFT JOIN LATERAL (
			SELECT email FROM core.user_emails 
			WHERE user_id = up.id 
			ORDER BY verified DESC, created_at ASC 
			LIMIT 1
		) AS ue ON true`).
		Where("pm.project_id = ?", projectID).
		Order("pm.created_at ASC").
		Scan(ctx, &members)

	if err != nil {
		r.log.Error("failed to list project members", logger.Error(err), slog.String("projectID", projectID))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}

	return members, nil
}

// CountAdmins counts the number of admins in a project
func (r *Repository) CountAdmins(ctx context.Context, projectID string) (int, error) {
	count, err := r.db.NewSelect().
		Model((*ProjectMembership)(nil)).
		Where("project_id = ?", projectID).
		Where("role = ?", RoleProjectAdmin).
		Count(ctx)

	if err != nil {
		r.log.Error("failed to count project admins", logger.Error(err), slog.String("projectID", projectID))
		return 0, apperror.ErrDatabase.WithInternal(err)
	}

	return count, nil
}

// RemoveMember removes a member from a project
func (r *Repository) RemoveMember(ctx context.Context, projectID, userID string) (bool, error) {
	result, err := r.db.NewDelete().
		Model((*ProjectMembership)(nil)).
		Where("project_id = ?", projectID).
		Where("user_id = ?", userID).
		Exec(ctx)

	if err != nil {
		r.log.Error("failed to remove project member", logger.Error(err),
			slog.String("projectID", projectID),
			slog.String("userID", userID))
		return false, apperror.ErrDatabase.WithInternal(err)
	}

	rowsAffected, _ := result.RowsAffected()
	return rowsAffected > 0, nil
}

// GetMembership returns a user's membership in a project
func (r *Repository) GetMembership(ctx context.Context, projectID, userID string) (*ProjectMembership, error) {
	var membership ProjectMembership

	err := r.db.NewSelect().
		Model(&membership).
		Where("project_id = ?", projectID).
		Where("user_id = ?", userID).
		Scan(ctx)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		r.log.Error("failed to get project membership", logger.Error(err))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}

	return &membership, nil
}

// IsUserMember checks if a user is a member of a project
func (r *Repository) IsUserMember(ctx context.Context, projectID, userID string) (bool, error) {
	exists, err := r.db.NewSelect().
		Model((*ProjectMembership)(nil)).
		Where("project_id = ?", projectID).
		Where("user_id = ?", userID).
		Exists(ctx)

	if err != nil {
		r.log.Error("failed to check project membership", logger.Error(err),
			slog.String("projectID", projectID),
			slog.String("userID", userID))
		return false, apperror.ErrDatabase.WithInternal(err)
	}

	return exists, nil
}

// BeginTx starts a new transaction.
// Returns a SafeTx that's safe to call Rollback after Commit (important for savepoints).
func (r *Repository) BeginTx(ctx context.Context) (*database.SafeTx, error) {
	tx, err := database.BeginSafeTx(ctx, r.db)
	if err != nil {
		r.log.Error("failed to begin transaction", logger.Error(err))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}
	return tx, nil
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
	errStr := err.Error()
	return len(errStr) > 0 && (strings.Contains(errStr, code) || strings.Contains(errStr, "SQLSTATE "+code))
}
