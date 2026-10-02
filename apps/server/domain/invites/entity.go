package invites

import (
	"time"

	"github.com/uptrace/bun"
)

// Invite represents an invitation record in the database
type Invite struct {
	bun.BaseModel `bun:"table:kb.invites,alias:i"`

	ID              string     `bun:"id,pk,type:uuid,default:uuid_generate_v4()" json:"id"`
	OrganizationID  string     `bun:"organization_id,type:uuid,notnull" json:"organizationId"`
	ProjectID       *string    `bun:"project_id,type:uuid" json:"projectId,omitempty"`
	Email           string     `bun:"email,notnull" json:"email"`
	Role            string     `bun:"role,notnull" json:"role"`
	Token           string     `bun:"token,notnull" json:"token"`
	Status          string     `bun:"status,notnull,default:'pending'" json:"status"`
	ExpiresAt       *time.Time `bun:"expires_at" json:"expiresAt,omitempty"`
	AcceptedAt      *time.Time `bun:"accepted_at" json:"acceptedAt,omitempty"`
	RevokedAt       *time.Time `bun:"revoked_at" json:"revokedAt,omitempty"`
	CreatedAt       time.Time  `bun:"created_at,notnull,default:now()" json:"createdAt"`
	InvitedByUserID *string    `bun:"invited_by_user_id,type:uuid" json:"invitedByUserId,omitempty"`
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
	ID               string     `json:"id"`
	Email            string     `json:"email"`
	Role             string     `json:"role"`
	Status           string     `json:"status"`
	CreatedAt        time.Time  `json:"createdAt"`
	ExpiresAt        *time.Time `json:"expiresAt,omitempty"`
	DeliveryStatus   *string    `json:"deliveryStatus,omitempty"`
	DeliveryStatusAt *time.Time `json:"deliveryStatusAt,omitempty"`
}

// ResendOutcome reports what a resend request actually did, so a caller (and the
// UI) can tell a real re-send from the idempotent within-window no-op instead of
// assuming every HTTP 200 sent an email (issue #1327).
type ResendOutcome string

const (
	// ResendOutcomeSent means a fresh project-invitation job was enqueued.
	ResendOutcomeSent ResendOutcome = "sent"
	// ResendOutcomeNoOp means the guard suppressed the request: a recent
	// invite-scoped job is still in flight or already succeeded, so no email
	// was enqueued.
	ResendOutcomeNoOp ResendOutcome = "noop"
	// ResendOutcomeEnqueueFailed means a send was attempted but the enqueue
	// failed; the request still succeeds (the invitation remains valid) but no
	// email was queued.
	ResendOutcomeEnqueueFailed ResendOutcome = "enqueue_failed"
)

// ResendResponse is the body of POST /api/invites/:id/resend. It carries the
// (possibly unchanged) invitation plus the outcome of the request. The Invite
// fields are embedded so existing consumers that read the invitation keep
// working; `outcome` is the added truthful signal.
type ResendResponse struct {
	Invite
	Outcome ResendOutcome `json:"outcome"`
}

// CreateInviteRequest is the request to create a new invite
type CreateInviteRequest struct {
	OrgID       string `json:"orgId"`
	ProjectID   string `json:"projectId"`
	Email       string `json:"email"`
	Role        string `json:"role"`
	InviterName string `json:"inviterName,omitempty"` // display name of the inviting user
	ProjectName string `json:"projectName,omitempty"` // display name of the project
	InviterID   string `json:"inviterId,omitempty"`   // user ID of the inviting user
}

// AcceptInviteRequest is the request to accept an invite
type AcceptInviteRequest struct {
	Token string `json:"token"`
}
