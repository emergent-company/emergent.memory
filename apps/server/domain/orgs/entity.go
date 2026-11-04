package orgs

import (
	"time"

	"github.com/uptrace/bun"
)

// Org represents an organization in the kb.orgs table
type Org struct {
	bun.BaseModel `bun:"table:kb.orgs,alias:o"`

	ID        string     `bun:"id,pk,type:uuid,default:uuid_generate_v4()" json:"id"`
	Name      string     `bun:"name,notnull" json:"name"`
	CreatedAt time.Time  `bun:"created_at,notnull,default:now()" json:"createdAt"`
	UpdatedAt time.Time  `bun:"updated_at,notnull,default:now()" json:"updatedAt"`
	DeletedAt *time.Time `bun:"deleted_at" json:"deletedAt,omitempty"`
	DeletedBy *string    `bun:"deleted_by,type:uuid" json:"deletedBy,omitempty"`
}

// OrganizationMembership represents a user's membership in an organization
type OrganizationMembership struct {
	bun.BaseModel `bun:"table:kb.organization_memberships,alias:om"`

	ID             string    `bun:"id,pk,type:uuid,default:uuid_generate_v4()" json:"id"`
	OrganizationID string    `bun:"organization_id,notnull,type:uuid" json:"organizationId"`
	UserID         string    `bun:"user_id,notnull,type:uuid" json:"userId"`
	Role           string    `bun:"role,notnull" json:"role"`
	CreatedAt      time.Time `bun:"created_at,notnull,default:now()" json:"createdAt"`

	// Relations (for joining)
	Organization *Org `bun:"rel:belongs-to,join:organization_id=id" json:"organization,omitempty"`
}

// OrgDTO is the response DTO for organization endpoints
type OrgDTO struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// OrgMemberDTO is the response DTO for org member endpoints
type OrgMemberDTO struct {
	ID          string  `json:"id"`
	Email       string  `json:"email"`
	DisplayName *string `json:"displayName,omitempty"`
	FirstName   *string `json:"firstName,omitempty"`
	LastName    *string `json:"lastName,omitempty"`
	Role        string  `json:"role"`
	JoinedAt    string  `json:"joinedAt"`

	// LastActiveAt is the newest observed authenticated request for this
	// member in this organization. Nil means no activity has been recorded.
	LastActiveAt *time.Time `json:"lastActiveAt,omitempty"`
	// ActivityTier is the recency bucket derived from LastActiveAt
	// (active | occasional | dormant | never).
	ActivityTier string `json:"activityTier,omitempty"`
}

// Canonical organization membership roles.
const (
	// OrgRoleAdmin grants organization administration (rename/delete, members,
	// invites, org settings).
	OrgRoleAdmin = "org_admin"
	// OrgRoleMember is a plain organization membership with no org-tier write
	// authority.
	OrgRoleMember = "member"
)

// AddOrgMemberRequest is the request body for directly adding an already
// registered user to an organization by exact verified email.
type AddOrgMemberRequest struct {
	Email string `json:"email" validate:"required"`
	Role  string `json:"role" validate:"required"`
}

// UpdateOrgMemberRoleRequest is the request body for changing an organization
// member's role.
type UpdateOrgMemberRoleRequest struct {
	Role string `json:"role" validate:"required"`
}

// CreateOrgRequest is the request body for creating an organization
type CreateOrgRequest struct {
	Name string `json:"name" validate:"required,min=1,max=120"`
}

// UpdateOrgRequest is the request body for updating an organization. Currently
// name-only: the Org entity has no description/logo column.
type UpdateOrgRequest struct {
	Name string `json:"name" validate:"required,min=1,max=120"`
}

// ToDTO converts an Org entity to OrgDTO
func (o *Org) ToDTO() OrgDTO {
	return OrgDTO{
		ID:   o.ID,
		Name: o.Name,
	}
}
