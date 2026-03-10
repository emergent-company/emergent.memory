package skills

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

// NamePattern is the validation regex for skill names: lowercase alphanumeric slugs with hyphens.
const NamePattern = `^[a-z0-9]+(-[a-z0-9]+)*$`

// Skill represents a row in kb.skills.
type Skill struct {
	bun.BaseModel `bun:"table:kb.skills,alias:s"`

	ID                   uuid.UUID      `bun:"id,pk,type:uuid,default:gen_random_uuid()" json:"id"`
	Name                 string         `bun:"name,notnull"                              json:"name"`
	Description          string         `bun:"description,notnull"                       json:"description"`
	Content              string         `bun:"content,notnull"                           json:"content"`
	Metadata             *SkillMetadata `bun:"metadata,type:jsonb"                       json:"metadata,omitempty"`
	DescriptionEmbedding []byte         `bun:"description_embedding,type:vector(768)"    json:"-"`
	ProjectID            *string        `bun:"project_id,type:uuid"                      json:"projectId,omitempty"`
	OrgID                *string        `bun:"org_id,type:uuid"                          json:"orgId,omitempty"`
	CreatedAt            time.Time      `bun:"created_at,notnull,default:now()"          json:"createdAt"`
	UpdatedAt            time.Time      `bun:"updated_at,notnull,default:now()"          json:"updatedAt"`
}

// SkillMetadata holds optional metadata fields stored as JSONB.
type SkillMetadata struct {
	Location string `json:"location,omitempty"` // source file path (e.g. from import)
}

// Scan implements sql.Scanner for SkillMetadata.
func (m *SkillMetadata) Scan(src interface{}) error {
	if src == nil {
		return nil
	}
	switch v := src.(type) {
	case []byte:
		return json.Unmarshal(v, m)
	case string:
		return json.Unmarshal([]byte(v), m)
	}
	return nil
}

// Scope returns the scope label for this skill.
func (s *Skill) Scope() string {
	if s.ProjectID != nil {
		return "project"
	}
	if s.OrgID != nil {
		return "org"
	}
	return "global"
}

// SkillDTO is the API response representation of a skill.
type SkillDTO struct {
	ID           string         `json:"id"`
	Name         string         `json:"name"`
	Description  string         `json:"description"`
	Content      string         `json:"content"`
	Metadata     *SkillMetadata `json:"metadata,omitempty"`
	HasEmbedding bool           `json:"hasEmbedding"`
	ProjectID    *string        `json:"projectId,omitempty"`
	OrgID        *string        `json:"orgId,omitempty"`
	Scope        string         `json:"scope"` // "global", "org", or "project"
	CreatedAt    string         `json:"createdAt"`
	UpdatedAt    string         `json:"updatedAt"`
}

// ToDTO converts a Skill to a SkillDTO.
func (s *Skill) ToDTO() *SkillDTO {
	return &SkillDTO{
		ID:           s.ID.String(),
		Name:         s.Name,
		Description:  s.Description,
		Content:      s.Content,
		Metadata:     s.Metadata,
		HasEmbedding: len(s.DescriptionEmbedding) > 0,
		ProjectID:    s.ProjectID,
		OrgID:        s.OrgID,
		Scope:        s.Scope(),
		CreatedAt:    s.CreatedAt.Format(time.RFC3339),
		UpdatedAt:    s.UpdatedAt.Format(time.RFC3339),
	}
}

// ListSkillsResponse is the paginated list response.
type ListSkillsResponse struct {
	Data []*SkillDTO `json:"skills"`
}

// CreateSkillDTO is the request body for creating a skill.
type CreateSkillDTO struct {
	Name        string         `json:"name"        validate:"required"`
	Description string         `json:"description"`
	Content     string         `json:"content"`
	Metadata    *SkillMetadata `json:"metadata,omitempty"`
}

// UpdateSkillDTO is the request body for partially updating a skill.
// Only non-nil fields are applied.
type UpdateSkillDTO struct {
	Description *string        `json:"description,omitempty"`
	Content     *string        `json:"content,omitempty"`
	Metadata    *SkillMetadata `json:"metadata,omitempty"`
}
