package invites

import (
	"time"

	"github.com/uptrace/bun"
)

// Invite represents an invitation record in the database
type Invite struct {
	bun.BaseModel `bun:"table:kb.invites,alias:i"`

	ID             string     `bun:"id,pk,type:uuid,default:uuid_generate_v4()" json:"id"`
	OrganizationID string     `bun:"organization_id,type:uuid,notnull" json:"organizationId"`
	ProjectID      *string    `bun:"project_id,type:uuid" json:"projectId,omitempty"`
	Email          string     `bun:"email,notnull" json:"email"`
	Role           string     `bun:"role,notnull" json:"role"`
	Token          string     `bun:"token,notnull" json:"token"`
	Status         string     `bun:"status,notnull,default:'pending'" json:"status"`
	ExpiresAt      *time.Time `bun:"expires_at" json:"expiresAt,omitempty"`
	AcceptedAt     *time.Time `bun:"accepted_at" json:"acceptedAt,omitempty"`
	RevokedAt      *time.Time `bun:"revoked_at" json:"revokedAt,omitempty"`
	CreatedAt      time.Time  `bun:"created_at,notnull,default:now()" json:"createdAt"`
}

// PendingInvite represents a pending invitation for a user
type PendingInvite struct {
	ID               string     `json:"id"`
	ProjectID        *string    `json:"projectId,omitempty"`
	ProjectName      *string    `json:"projectName,omitempty"`
	OrganizationID   string     `json:"organizationId"`
	OrganizationName *string    `json:"organizationName,omitempty"`
	Role             string     `json:"role"`
	Token            string     `json:"token"`
	CreatedAt        time.Time  `json:"createdAt"`
	ExpiresAt        *time.Time `json:"expiresAt,omitempty"`
}

// SentInvite represents an invite sent by a project (for project members page)
type SentInvite struct {
	ID        string     `json:"id"`
	Email     string     `json:"email"`
	Role      string     `json:"role"`
	Status    string     `json:"status"`
	CreatedAt time.Time  `json:"createdAt"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
}

// CreateInviteRequest is the request to create a new invite
type CreateInviteRequest struct {
	OrgID     string `json:"orgId"`
	ProjectID string `json:"projectId"`
	Email     string `json:"email"`
	Role      string `json:"role"`
}

// AcceptInviteRequest is the request to accept an invite
type AcceptInviteRequest struct {
	Token string `json:"token"`
}
