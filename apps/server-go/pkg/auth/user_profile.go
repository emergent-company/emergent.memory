package auth

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent/pkg/logger"
)

// UserProfile represents a user profile from the database
type UserProfile struct {
	bun.BaseModel `bun:"table:core.user_profiles"`
	ID            string     `bun:"id,pk,type:uuid"`
	ZitadelUserID string     `bun:"zitadel_user_id"`
	FirstName     *string    `bun:"first_name"`
	LastName      *string    `bun:"last_name"`
	DisplayName   *string    `bun:"display_name"`
	Email         string     `bun:"-"` // Fetched separately from user_emails
	CreatedAt     time.Time  `bun:"created_at"`
	UpdatedAt     time.Time  `bun:"updated_at"`
	DeletedAt     *time.Time `bun:"deleted_at"`
	DeletedBy     *string    `bun:"deleted_by"`
}

// UserProfileInfo contains optional profile information for upsert
type UserProfileInfo struct {
	FirstName   string
	LastName    string
	DisplayName string
	Email       string
}

// UserProfileService handles user profile operations
type UserProfileService struct {
	db  bun.IDB
	log *slog.Logger
}

// NewUserProfileService creates a new user profile service
func NewUserProfileService(db bun.IDB, log *slog.Logger) *UserProfileService {
	return &UserProfileService{
		db:  db,
		log: log.With(logger.Scope("user-profile")),
	}
}

// GetByID retrieves a user profile by internal ID
func (s *UserProfileService) GetByID(ctx context.Context, id string) (*UserProfile, error) {
	var profile UserProfile
	err := s.db.NewSelect().
		TableExpr("core.user_profiles").
		Column("id", "zitadel_user_id", "first_name", "last_name", "display_name", "created_at", "updated_at", "deleted_at", "deleted_by").
		Where("id = ?", id).
		Where("deleted_at IS NULL").
		Scan(ctx, &profile)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errors.New("user not found")
		}
		return nil, err
	}

	// Fetch email
	profile.Email, _ = s.getEmail(ctx, profile.ID)

	return &profile, nil
}

// GetByZitadelUserID retrieves a user profile by Zitadel user ID
func (s *UserProfileService) GetByZitadelUserID(ctx context.Context, zitadelUserID string) (*UserProfile, error) {
	var profile UserProfile
	err := s.db.NewSelect().
		TableExpr("core.user_profiles").
		Column("id", "zitadel_user_id", "first_name", "last_name", "display_name", "created_at", "updated_at", "deleted_at", "deleted_by").
		Where("zitadel_user_id = ?", zitadelUserID).
		Where("deleted_at IS NULL").
		Scan(ctx, &profile)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil // Not found, not an error
		}
		return nil, err
	}

	// Fetch email
	profile.Email, _ = s.getEmail(ctx, profile.ID)

	return &profile, nil
}

// EnsureProfile ensures a user profile exists for the given subject ID
// This matches the NestJS upsertBase behavior
func (s *UserProfileService) EnsureProfile(ctx context.Context, subjectID string, info *UserProfileInfo) (*UserProfile, error) {
	// 1. Check for active (non-deleted) user profile
	profile, err := s.GetByZitadelUserID(ctx, subjectID)
	if err != nil {
		return nil, err
	}

	if profile != nil {
		// Profile exists and is active, sync email if provided
		if info != nil && info.Email != "" {
			_ = s.syncEmail(ctx, profile.ID, info.Email)
		}
		return profile, nil
	}

	// 2. Check for soft-deleted profile to reactivate
	var deletedProfile UserProfile
	err = s.db.NewSelect().
		TableExpr("core.user_profiles").
		Column("id", "zitadel_user_id", "first_name", "last_name", "display_name", "created_at", "updated_at", "deleted_at", "deleted_by").
		Where("zitadel_user_id = ?", subjectID).
		Where("deleted_at IS NOT NULL").
		Scan(ctx, &deletedProfile)

	if err == nil && deletedProfile.ID != "" {
		// Reactivate the profile
		s.log.Info("reactivating soft-deleted user profile",
			slog.String("profile_id", deletedProfile.ID),
			slog.String("zitadel_user_id", subjectID),
		)

		_, err = s.db.NewUpdate().
			TableExpr("core.user_profiles").
			Set("deleted_at = NULL").
			Set("deleted_by = NULL").
			Set("welcome_email_sent_at = NULL").
			Set("updated_at = NOW()").
			Where("id = ?", deletedProfile.ID).
			Exec(ctx)

		if err != nil {
			return nil, err
		}

		deletedProfile.DeletedAt = nil
		deletedProfile.DeletedBy = nil

		// Sync email if provided
		if info != nil && info.Email != "" {
			_ = s.syncEmail(ctx, deletedProfile.ID, info.Email)
			deletedProfile.Email = info.Email
		}

		return &deletedProfile, nil
	}

	// 3. Create new profile (with conflict handling for race conditions)
	s.log.Info("creating new user profile",
		slog.String("zitadel_user_id", subjectID),
	)

	newProfile := &UserProfile{
		ZitadelUserID: subjectID,
	}

	if info != nil {
		if info.FirstName != "" {
			newProfile.FirstName = &info.FirstName
		}
		if info.LastName != "" {
			newProfile.LastName = &info.LastName
		}
		if info.DisplayName != "" {
			newProfile.DisplayName = &info.DisplayName
		}
	}

	err = s.db.NewInsert().
		TableExpr("core.user_profiles").
		Model(newProfile).
		ExcludeColumn("id"). // Let database generate UUID
		On("CONFLICT (zitadel_user_id) DO UPDATE").
		Set("updated_at = NOW()").
		Returning("id, created_at, updated_at").
		Scan(ctx) // Use Scan to populate newProfile with RETURNING values

	if err != nil {
		return nil, err
	}

	// Sync email if provided
	if info != nil && info.Email != "" {
		_ = s.syncEmail(ctx, newProfile.ID, info.Email)
		newProfile.Email = info.Email
	}

	return newProfile, nil
}

// getEmail retrieves the verified email for a user
func (s *UserProfileService) getEmail(ctx context.Context, userID string) (string, error) {
	var email string
	err := s.db.NewSelect().
		TableExpr("core.user_emails").
		Column("email").
		Where("user_id = ?", userID).
		Where("verified = true").
		Limit(1).
		Scan(ctx, &email)

	if err != nil {
		return "", err
	}
	return email, nil
}

// syncEmail syncs the user's email to the user_emails table
func (s *UserProfileService) syncEmail(ctx context.Context, userID, email string) error {
	if email == "" {
		return nil
	}

	// Normalize email
	email = strings.TrimSpace(strings.ToLower(email))

	// Check if email already exists for this user
	var existingID string
	err := s.db.NewSelect().
		TableExpr("core.user_emails").
		Column("id").
		Where("user_id = ?", userID).
		Where("email = ?", email).
		Scan(ctx, &existingID)

	if err == nil && existingID != "" {
		// Email already exists
		return nil
	}

	// Insert new email (unique constraint is on email only, not user_id+email)
	_, err = s.db.NewInsert().
		TableExpr("core.user_emails").
		Model(&struct {
			UserID   string `bun:"user_id,type:uuid"`
			Email    string `bun:"email"`
			Verified bool   `bun:"verified"`
		}{
			UserID:   userID,
			Email:    email,
			Verified: true,
		}).
		On("CONFLICT (email) DO NOTHING").
		Exec(ctx)

	return err
}
