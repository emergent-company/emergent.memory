package blueprints

import (
	"encoding/json"
	"time"

	"github.com/uptrace/bun"
)

// Blueprint status values.
const (
	StatusDraft      = "draft"
	StatusPublished  = "published"
	StatusDeprecated = "deprecated"
)

// Blueprint is a versioned, immutable-on-publish blueprint definition.
// Manifest is stored as opaque JSON; its contents are parsed and validated
// in a later apply phase. Checksum is a content hash of the JSON manifest
// used for future apply/drift comparison, not verified on read.
// ProjectID is nil for global (shared built-in) blueprints; set to a project
// UUID for private blueprints visible only to that project.
type Blueprint struct {
	bun.BaseModel `bun:"table:kb.blueprints,alias:bp"`

	ID          string          `bun:"id,pk,type:uuid,default:uuid_generate_v4()" json:"id"`
	Name        string          `bun:"name,notnull" json:"name"`
	Version     string          `bun:"version,notnull" json:"version"`
	Description string          `bun:"description,notnull,default:''" json:"description"`
	Author      string          `bun:"author,notnull,default:''" json:"author"`
	Status      string          `bun:"status,notnull,default:'draft'" json:"status"`
	ProjectID   *string         `bun:"project_id,type:uuid" json:"projectId,omitempty"`
	Manifest    json.RawMessage `bun:"manifest,type:jsonb" json:"manifest"`
	Checksum    string          `bun:"checksum,notnull,default:''" json:"checksum"`
	SourceKind  string          `bun:"source_kind,type:text" json:"sourceKind,omitempty"`
	SourceURL   string          `bun:"source_url,type:text" json:"sourceUrl,omitempty"`
	SourceRef   string          `bun:"source_ref,type:text" json:"sourceRef,omitempty"`
	CreatedAt   time.Time       `bun:"created_at,notnull" json:"created_at"`
	UpdatedAt   time.Time       `bun:"updated_at,notnull" json:"updated_at"`
}

// Scope reports the blueprint's visibility scope: "project" for private
// blueprints, "global" for shared built-ins.
func (b *Blueprint) Scope() string {
	if b.ProjectID != nil {
		return "project"
	}
	return "global"
}

// CreateBlueprintRequest is the request to create a new blueprint draft.
// ProjectID nil = global; set = private to that project.
type CreateBlueprintRequest struct {
	Name        string          `json:"name"`
	Version     string          `json:"version"`
	Description string          `json:"description,omitempty"`
	Author      string          `json:"author,omitempty"`
	ProjectID   *string         `json:"projectId,omitempty"`
	Manifest    json.RawMessage `json:"manifest"`
	SourceKind  string          `json:"sourceKind,omitempty"`
	SourceURL   string          `json:"sourceUrl,omitempty"`
	SourceRef   string          `json:"sourceRef,omitempty"`
}

// UpdateBlueprintRequest is the request to update a draft blueprint.
// Only non-nil fields are applied; Manifest applies when non-nil.
type UpdateBlueprintRequest struct {
	Description *string         `json:"description,omitempty"`
	Author      *string         `json:"author,omitempty"`
	Manifest    json.RawMessage `json:"manifest"`
}

// NewVersionRequest is the request to clone a blueprint into a new version.
type NewVersionRequest struct {
	Version string `json:"version"`
}

// BlueprintApplication records one project's application of a blueprint version.
// (project_id, blueprint_id) is unique — a re-apply updates the row — so the
// table reflects "currently applied" state, not append-only history. Checksum
// is the applied manifest content hash (empty for a draft apply), kept for
// future drift detection and unapply.
type BlueprintApplication struct {
	bun.BaseModel `bun:"table:kb.blueprint_applications,alias:bpa"`

	ID          string    `bun:"id,pk,type:uuid,default:uuid_generate_v4()" json:"id"`
	BlueprintID string    `bun:"blueprint_id,notnull,type:uuid" json:"blueprintId"`
	ProjectID   string    `bun:"project_id,notnull,type:uuid" json:"projectId"`
	Version     string    `bun:"version,notnull" json:"version"`
	Checksum    string    `bun:"checksum,notnull,default:''" json:"checksum"`
	AppliedBy   *string   `bun:"applied_by,type:uuid" json:"appliedBy,omitempty"`
	Status      string    `bun:"status,notnull,default:'applied'" json:"status"`
	AppliedAt   time.Time `bun:"applied_at,notnull,default:now()" json:"appliedAt"`
	CreatedAt   time.Time `bun:"created_at,notnull,default:now()" json:"createdAt"`
	UpdatedAt   time.Time `bun:"updated_at,notnull,default:now()" json:"updatedAt"`
}

// BlueprintPackClaim records that a blueprint's apply depends on a schema pack
// for a project. One row per (blueprint, project, pack); unapply uses it to
// avoid removing a pack assignment shared with another applied blueprint.
// Table: kb.blueprint_pack_claims.
type BlueprintPackClaim struct {
	bun.BaseModel `bun:"table:kb.blueprint_pack_claims,alias:bpc"`

	ID          string    `bun:"id,pk,type:uuid,default:uuid_generate_v4()" json:"id"`
	BlueprintID string    `bun:"blueprint_id,notnull,type:uuid" json:"blueprintId"`
	ProjectID   string    `bun:"project_id,notnull,type:uuid" json:"projectId"`
	SchemaID    string    `bun:"schema_id,notnull,type:uuid" json:"schemaId"`
	CreatedAt   time.Time `bun:"created_at,notnull,default:now()" json:"createdAt"`
}

// AppliedBlueprint is the project-scoped view of an applied blueprint, joined
// with its blueprint metadata. Returned by GET /api/blueprints/applied.
type AppliedBlueprint struct {
	BlueprintID string    `json:"blueprintId"`
	Name        string    `json:"name"`
	Version     string    `json:"version"`
	Description string    `json:"description,omitempty"`
	Author      string    `json:"author,omitempty"`
	Checksum    string    `json:"checksum"`
	AppliedAt   time.Time `json:"appliedAt"`
}

// UnapplyCounts reports per-entity-type reversal counts for one blueprint
// unapply. Missing counts entities that were already absent (idempotent);
// Skipped counts entities intentionally not reversed.
type UnapplyCounts struct {
	Removed int `json:"removed"`
	Missing int `json:"missing"`
	Skipped int `json:"skipped"`
}

// UnapplyResult is the response from Unapply.
type UnapplyResult struct {
	BlueprintID      string        `json:"blueprintId"`
	BlueprintName    string        `json:"blueprintName"`
	Version          string        `json:"version"`
	Checksum         string        `json:"checksum"`         // stored (applied) checksum
	Drift            bool          `json:"drift"`            // applied checksum != current blueprint checksum
	AlreadyUnapplied bool          `json:"alreadyUnapplied"` // idempotent short-circuit
	DriftedAgents    []string      `json:"driftedAgents,omitempty"`
	Packs            UnapplyCounts `json:"packs"`
	Agents           UnapplyCounts `json:"agents"`
	Skills           UnapplyCounts `json:"skills"`
	SkippedSkills    []string      `json:"skippedSkills,omitempty"`
	Seed             UnapplyCounts `json:"seed"`
	Status           string        `json:"status"`
}
