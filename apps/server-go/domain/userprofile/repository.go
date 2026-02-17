package userprofile

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"time"

	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent/pkg/apperror"
	"github.com/emergent-company/emergent/pkg/logger"
)

// Repository handles database operations for user profiles
type Repository struct {
	db  bun.IDB
	log *slog.Logger
}

// NewRepository creates a new user profile repository
func NewRepository(db bun.IDB, log *slog.Logger) *Repository {
	return &Repository{
		db:  db,
		log: log.With(logger.Scope("userprofile.repo")),
	}
}

// GetByID retrieves a user profile by internal ID
func (r *Repository) GetByID(ctx context.Context, id string) (*Profile, error) {
	var profile Profile
	err := r.db.NewSelect().
		Model(&profile).
		Where("id = ?", id).
		Where("deleted_at IS NULL").
		Scan(ctx)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, apperror.ErrNotFound.WithMessage("user profile not found")
		}
		r.log.Error("failed to get profile by id", logger.Error(err))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}

	return &profile, nil
}

// GetByZitadelUserID retrieves a user profile by Zitadel user ID
func (r *Repository) GetByZitadelUserID(ctx context.Context, zitadelUserID string) (*Profile, error) {
	var profile Profile
	err := r.db.NewSelect().
		Model(&profile).
		Where("zitadel_user_id = ?", zitadelUserID).
		Where("deleted_at IS NULL").
		Scan(ctx)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, apperror.ErrNotFound.WithMessage("user profile not found")
		}
		r.log.Error("failed to get profile by zitadel user id", logger.Error(err))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}

	return &profile, nil
}

// Update updates a user profile
func (r *Repository) Update(ctx context.Context, id string, req *UpdateProfileRequest) (*Profile, error) {
	// Build update query dynamically based on provided fields
	query := r.db.NewUpdate().
		Model((*Profile)(nil)).
		Where("id = ?", id).
		Where("deleted_at IS NULL").
		Set("updated_at = ?", time.Now())

	// Only update fields that are provided (not nil)
	if req.FirstName != nil {
		query = query.Set("first_name = ?", req.FirstName)
	}
	if req.LastName != nil {
		query = query.Set("last_name = ?", req.LastName)
	}
	if req.DisplayName != nil {
		query = query.Set("display_name = ?", req.DisplayName)
	}
	if req.PhoneE164 != nil {
		query = query.Set("phone_e164 = ?", req.PhoneE164)
	}

	result, err := query.Exec(ctx)
	if err != nil {
		r.log.Error("failed to update profile", logger.Error(err))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return nil, apperror.ErrNotFound.WithMessage("user profile not found")
	}

	// Fetch and return the updated profile
	return r.GetByID(ctx, id)
}

// GetEmail retrieves the primary verified email for a user
func (r *Repository) GetEmail(ctx context.Context, userID string) (string, error) {
	var email string
	err := r.db.NewSelect().
		TableExpr("core.user_emails").
		Column("email").
		Where("user_id = ?", userID).
		Where("verified = true").
		OrderExpr("created_at ASC"). // First verified email is primary
		Limit(1).
		Scan(ctx, &email)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", err
	}
	return email, nil
}
