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

// CreateOrgRequest is the request body for creating an organization
type CreateOrgRequest struct {
	Name string `json:"name" validate:"required,min=1,max=120"`
}

// ToDTO converts an Org entity to OrgDTO
func (o *Org) ToDTO() OrgDTO {
	return OrgDTO{
		ID:   o.ID,
		Name: o.Name,
	}
}
