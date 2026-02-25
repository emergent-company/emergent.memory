package projects

import (
	"time"

	"github.com/uptrace/bun"
)

// Project represents a project in the kb.projects table
// Note: Only includes columns from the squashed initial migration.
// Additional columns (chunking_config, allow_parallel_extraction, extraction_config,
// deleted_at, deleted_by) are added in later migrations and should be added here
// when needed with appropriate migration checks.
type Project struct {
	bun.BaseModel `bun:"table:kb.projects,alias:p"`

	ID                 string         `bun:"id,pk,type:uuid,default:gen_random_uuid()" json:"id"`
	OrganizationID     string         `bun:"organization_id,notnull,type:uuid" json:"organizationId"`
	Name               string         `bun:"name,notnull" json:"name"`
	KBPurpose          *string        `bun:"kb_purpose" json:"kb_purpose,omitempty"`
	ChatPromptTemplate *string        `bun:"chat_prompt_template" json:"chat_prompt_template,omitempty"`
	AutoExtractObjects bool           `bun:"auto_extract_objects,notnull,default:false" json:"auto_extract_objects"`
	AutoExtractConfig  map[string]any `bun:"auto_extract_config,type:jsonb,default:'{}'" json:"auto_extract_config,omitempty"`
	CreatedAt          time.Time      `bun:"created_at,notnull,default:now()" json:"createdAt"`
	UpdatedAt          time.Time      `bun:"updated_at,notnull,default:now()" json:"updatedAt"`

	// Populated only when requested
	Stats *ProjectStats `bun:"-" json:"stats,omitempty"`
}

// ChunkingConfig represents the chunking configuration for a project
// Note: This column is added in migration 1763120000000-AddProjectChunkingConfig
// Keeping for future use when we ensure all migrations run
type ChunkingConfig struct {
	Strategy     string `json:"strategy,omitempty"`     // "character" | "sentence" | "paragraph"
	MaxChunkSize *int   `json:"maxChunkSize,omitempty"` // 100-25000
	MinChunkSize *int   `json:"minChunkSize,omitempty"` // 10-10000
	Overlap      *int   `json:"overlap,omitempty"`      // 0-500
}

// ExtractionConfig represents the extraction configuration for a project
// Note: This column may be added in a later migration
// Keeping for future use when we ensure all migrations run
type ExtractionConfig struct {
	ChunkSize      *int    `json:"chunkSize,omitempty"`      // 5000-100000
	Method         *string `json:"method,omitempty"`         // "json_freeform" | "function_calling" | "responseSchema"
	TimeoutSeconds *int    `json:"timeoutSeconds,omitempty"` // 60-600
}

// ProjectMembership represents a user's membership in a project
type ProjectMembership struct {
	bun.BaseModel `bun:"table:kb.project_memberships,alias:pm"`

	ID        string    `bun:"id,pk,type:uuid,default:gen_random_uuid()" json:"id"`
	ProjectID string    `bun:"project_id,notnull,type:uuid" json:"projectId"`
	UserID    string    `bun:"user_id,notnull,type:uuid" json:"userId"`
	Role      string    `bun:"role,notnull" json:"role"` // "project_admin" | "project_user"
	CreatedAt time.Time `bun:"created_at,notnull,default:now()" json:"createdAt"`

	// Relations (for joining)
	Project *Project `bun:"rel:belongs-to,join:project_id=id" json:"project,omitempty"`
}

// Role constants
const (
	RoleProjectAdmin = "project_admin"
	RoleProjectUser  = "project_user"
)

// TemplatePack represents an installed template pack for a project
type TemplatePack struct {
	Name              string   `json:"name"`
	Version           string   `json:"version"`
	ObjectTypes       []string `json:"objectTypes"`
	RelationshipTypes []string `json:"relationshipTypes"`
}

// ProjectStats represents aggregated statistics for a project
type ProjectStats struct {
	DocumentCount     int            `json:"documentCount"`
	ObjectCount       int            `json:"objectCount"`
	RelationshipCount int            `json:"relationshipCount"`
	TotalJobs         int            `json:"totalJobs"`
	RunningJobs       int            `json:"runningJobs"`
	QueuedJobs        int            `json:"queuedJobs"`
	TemplatePacks     []TemplatePack `json:"templatePacks"`
}

// ProjectDTO is the response DTO for project endpoints
type ProjectDTO struct {
	ID                 string         `json:"id"`
	Name               string         `json:"name"`
	OrgID              string         `json:"orgId"`
	KBPurpose          *string        `json:"kb_purpose,omitempty"`
	ChatPromptTemplate *string        `json:"chat_prompt_template,omitempty"`
	AutoExtractObjects *bool          `json:"auto_extract_objects,omitempty"`
	AutoExtractConfig  map[string]any `json:"auto_extract_config,omitempty"`
	Stats              *ProjectStats  `json:"stats,omitempty"`
}

// ProjectMemberDTO is the response DTO for project member endpoints
type ProjectMemberDTO struct {
	ID          string    `json:"id"`
	Email       string    `json:"email"`
	DisplayName *string   `json:"displayName,omitempty"`
	FirstName   *string   `json:"firstName,omitempty"`
	LastName    *string   `json:"lastName,omitempty"`
	AvatarURL   *string   `json:"avatarUrl,omitempty"`
	Role        string    `json:"role"`
	JoinedAt    time.Time `json:"joinedAt"`
}

// CreateProjectRequest is the request body for creating a project
type CreateProjectRequest struct {
	Name  string `json:"name" validate:"required,min=1"`
	OrgID string `json:"orgId" validate:"required,uuid"`
}

// UpdateProjectRequest is the request body for updating a project
type UpdateProjectRequest struct {
	Name               *string        `json:"name,omitempty" validate:"omitempty,min=1"`
	KBPurpose          *string        `json:"kb_purpose,omitempty"`
	ChatPromptTemplate *string        `json:"chat_prompt_template,omitempty"`
	AutoExtractObjects *bool          `json:"auto_extract_objects,omitempty"`
	AutoExtractConfig  map[string]any `json:"auto_extract_config,omitempty"`
}

// ToDTO converts a Project entity to ProjectDTO
// Note: Stats are not populated here, they must be set separately after querying
func (p *Project) ToDTO() ProjectDTO {
	dto := ProjectDTO{
		ID:                 p.ID,
		Name:               p.Name,
		OrgID:              p.OrganizationID,
		KBPurpose:          p.KBPurpose,
		ChatPromptTemplate: p.ChatPromptTemplate,
		Stats:              p.Stats,
	}

	// Only include boolean fields if they are true (to match NestJS behavior)
	if p.AutoExtractObjects {
		val := p.AutoExtractObjects
		dto.AutoExtractObjects = &val
	}

	// Include config fields if they exist
	if len(p.AutoExtractConfig) > 0 {
		dto.AutoExtractConfig = p.AutoExtractConfig
	}

	return dto
}
