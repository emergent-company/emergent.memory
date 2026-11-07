package skills

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

// NamePattern is the validation regex for skill names: lowercase alphanumeric
// segments separated by a single hyphen or dot. Dots are allowed because
// AutoLoadSkills matches learned skills by the "{agentSlug}." prefix (see
// AgentSkillPrefix); the loader and the writer must agree on the shape.
const NamePattern = `^[a-z0-9]+([-.][a-z0-9]+)*$`

// Provenance source values for SkillMetadata.Source.
const (
	SourceManual      = "manual"
	SourceCLI         = "cli"
	SourceBlueprint   = "blueprint"
	SourceAgent       = "agent"
	SourceMarketplace = "marketplace"
)

// Skill represents a row in kb.skills.
type Skill struct {
	bun.BaseModel `bun:"table:kb.skills,alias:s"`

	ID                   uuid.UUID      `bun:"id,pk,type:uuid,default:gen_random_uuid()" json:"id"`
	Name                 string         `bun:"name,notnull"                              json:"name"`
	Description          string         `bun:"description,notnull"                       json:"description"`
	Content              string         `bun:"content,notnull"                           json:"content"`
	Metadata             *SkillMetadata `bun:"metadata,type:jsonb"                       json:"metadata,omitempty"`
	DescriptionEmbedding []byte         `bun:"description_embedding,type:vector(768),nullzero" json:"-"`
	ProjectID            *string        `bun:"project_id,type:uuid"                      json:"projectId,omitempty"`
	OrgID                *string        `bun:"org_id,type:uuid"                          json:"orgId,omitempty"`
	CreatedAt            time.Time      `bun:"created_at,notnull,default:now()"          json:"createdAt"`
	UpdatedAt            time.Time      `bun:"updated_at,notnull,default:now()"          json:"updatedAt"`
}

// SkillMetadata holds optional metadata fields stored as JSONB.
type SkillMetadata struct {
	Location string `json:"location,omitempty"` // source file path (e.g. from import)

	// Provenance fields. These are preserved verbatim by the server on update and
	// never inferred or overwritten, with the exception of ContentHash which is
	// always computed server-side from the skill content at write time.
	Source      string `json:"source,omitempty"`       // manual | cli | blueprint | agent | marketplace
	License     string `json:"license,omitempty"`      // SPDX identifier or free text
	Version     string `json:"version,omitempty"`      // skill version string
	SourceURL   string `json:"source_url,omitempty"`   // URL the skill was imported from
	OriginID    string `json:"origin_id,omitempty"`    // external identifier of the source skill
	ContentHash string `json:"content_hash,omitempty"` // SHA-256 of content, computed server-side

	// Capture provenance. Populated when an agent-created skill is applied from
	// a proposal; empty for user-authored skills.
	Author    string `json:"author,omitempty"`     // display name of the creating agent
	RunID     string `json:"run_id,omitempty"`     // source agent run
	CreatedBy string `json:"created_by,omitempty"` // "agent" | "user" | "cli" | "blueprint"

	// Lifecycle guards. Pinned skills are never aged by the curator; locked
	// skills refuse agent-initiated writes and deletes (human REST writes still
	// work). Both default to false.
	Pinned bool `json:"pinned,omitempty"`
	Locked bool `json:"locked,omitempty"`

	// LifecycleState is maintained by the deterministic curator for
	// agent-created skills: active | stale | archived. Archived skills are not
	// auto-loaded or semantically retrieved; they remain readable by name.
	LifecycleState string `json:"lifecycle_state,omitempty"`
}

// Lifecycle states for agent-created skills.
const (
	SkillLifecycleActive   = "active"
	SkillLifecycleStale    = "stale"
	SkillLifecycleArchived = "archived"
)

// LifecycleStateOrDefault returns the stored state, defaulting to active.
func (s *Skill) LifecycleStateOrDefault() string {
	if s == nil || s.Metadata == nil || s.Metadata.LifecycleState == "" {
		return SkillLifecycleActive
	}
	return s.Metadata.LifecycleState
}

// IsAgentCreated reports whether the skill was written by the skill-capture
// loop. Only these skills are eligible for curator ageing and auto-patches.
func (s *Skill) IsAgentCreated() bool {
	return s != nil && s.Metadata != nil && (s.Metadata.CreatedBy == "agent" || s.Metadata.Source == SourceAgent)
}

// IsPinned reports the pin guard (nil-safe).
func (s *Skill) IsPinned() bool {
	return s != nil && s.Metadata != nil && s.Metadata.Pinned
}

// IsLocked reports the lock guard (nil-safe).
func (s *Skill) IsLocked() bool {
	return s != nil && s.Metadata != nil && s.Metadata.Locked
}

// ContentHashOf returns the stored content hash, or "" when absent.
func (s *Skill) ContentHashOf() string {
	if s == nil || s.Metadata == nil {
		return ""
	}
	return s.Metadata.ContentHash
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
