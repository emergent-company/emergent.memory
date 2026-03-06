package userprofile

import (
	"time"

	"github.com/uptrace/bun"
)

// Profile represents a user profile from core.user_profiles
type Profile struct {
	bun.BaseModel `bun:"table:core.user_profiles,alias:up"`

	ID              string     `bun:"id,pk,type:uuid"`
	ZitadelUserID   string     `bun:"zitadel_user_id"`
	FirstName       *string    `bun:"first_name"`
	LastName        *string    `bun:"last_name"`
	DisplayName     *string    `bun:"display_name"`
	PhoneE164       *string    `bun:"phone_e164"`
	AvatarObjectKey *string    `bun:"avatar_object_key"`
	CreatedAt       time.Time  `bun:"created_at"`
	UpdatedAt       time.Time  `bun:"updated_at"`
	DeletedAt       *time.Time `bun:"deleted_at"`
	DeletedBy       *string    `bun:"deleted_by,type:uuid"`
}

// ProfileDTO is the response DTO for the user profile endpoint
type ProfileDTO struct {
	ID              string  `json:"id"`
	SubjectID       string  `json:"subjectId"`
	ZitadelUserID   *string `json:"zitadelUserId,omitempty"`
	FirstName       *string `json:"firstName,omitempty"`
	LastName        *string `json:"lastName,omitempty"`
	DisplayName     *string `json:"displayName,omitempty"`
	PhoneE164       *string `json:"phoneE164,omitempty"`
	AvatarObjectKey *string `json:"avatarObjectKey,omitempty"`
	Email           string  `json:"email,omitempty"`
}

// UpdateProfileRequest is the request body for updating profile
type UpdateProfileRequest struct {
	FirstName   *string `json:"firstName,omitempty"`
	LastName    *string `json:"lastName,omitempty"`
	DisplayName *string `json:"displayName,omitempty"`
	PhoneE164   *string `json:"phoneE164,omitempty"`
}

// ToDTO converts a Profile entity to ProfileDTO
func (p *Profile) ToDTO(email string) ProfileDTO {
	return ProfileDTO{
		ID:              p.ID,
		SubjectID:       p.ZitadelUserID,
		ZitadelUserID:   &p.ZitadelUserID,
		FirstName:       p.FirstName,
		LastName:        p.LastName,
		DisplayName:     p.DisplayName,
		PhoneE164:       p.PhoneE164,
		AvatarObjectKey: p.AvatarObjectKey,
		Email:           email,
	}
}
