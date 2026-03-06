package users

import (
	"time"

	"github.com/uptrace/bun"
)

// UserProfile represents a user profile from core.user_profiles
// This entity is used for user search and listing
type UserProfile struct {
	bun.BaseModel `bun:"table:core.user_profiles,alias:up"`

	ID              string     `bun:"id,pk,type:uuid" json:"id"`
	ZitadelUserID   string     `bun:"zitadel_user_id" json:"-"`
	FirstName       *string    `bun:"first_name" json:"firstName,omitempty"`
	LastName        *string    `bun:"last_name" json:"lastName,omitempty"`
	DisplayName     *string    `bun:"display_name" json:"displayName,omitempty"`
	AvatarObjectKey *string    `bun:"avatar_object_key" json:"-"`
	CreatedAt       time.Time  `bun:"created_at" json:"-"`
	UpdatedAt       time.Time  `bun:"updated_at" json:"-"`
	DeletedAt       *time.Time `bun:"deleted_at" json:"-"`
}

// UserEmail represents an email in core.user_emails
type UserEmail struct {
	bun.BaseModel `bun:"table:core.user_emails,alias:ue"`

	ID        string    `bun:"id,pk,type:uuid"`
	UserID    string    `bun:"user_id,type:uuid"`
	Email     string    `bun:"email"`
	Verified  bool      `bun:"verified"`
	CreatedAt time.Time `bun:"created_at"`
}

// UserSearchResult is the DTO for a single user in search results
type UserSearchResult struct {
	ID              string  `json:"id"`
	Email           string  `json:"email"`
	DisplayName     *string `json:"displayName,omitempty"`
	FirstName       *string `json:"firstName,omitempty"`
	LastName        *string `json:"lastName,omitempty"`
	AvatarObjectKey *string `json:"avatarObjectKey,omitempty"`
}

// UserSearchResponse is the response DTO for user search
type UserSearchResponse struct {
	Users []UserSearchResult `json:"users"`
}
