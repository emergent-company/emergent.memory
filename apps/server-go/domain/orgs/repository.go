package orgs

import (
	"context"
	"database/sql"
	"log/slog"

	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent/internal/database"
	"github.com/emergent-company/emergent/pkg/apperror"
	"github.com/emergent-company/emergent/pkg/logger"
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
		return nil, apperror.ErrDatabase.WithInternal(err)
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
			return nil, apperror.ErrNotFound.WithMessage("Organization not found")
		}
		r.log.Error("failed to get organization", logger.Error(err), slog.String("id", id))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}

	return &org, nil
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
