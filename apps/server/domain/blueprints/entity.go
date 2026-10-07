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

// Blueprint origin values. Origin distinguishes a platform built-in
// (project_id IS NULL, seeded at boot) from a user-authored blueprint and a
// GitHub import; it drives apply-time pack scoping (builtin → global pack,
// user/import → project-scoped pack).
const (
	OriginBuiltin = "builtin"
	OriginUser    = "user"
	OriginImport  = "import"
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
	// SourceManifestChecksum is the canonical hash of the raw upstream manifest
	// at import time, before project model overrides. Update checks compare
	// against it so overridden imports do not report a false-positive update.
	// Internal only — never serialized.
	SourceManifestChecksum string    `bun:"source_manifest_checksum,type:text" json:"-"`
	Origin                 string    `bun:"origin,notnull,default:'user'" json:"origin"`
	CreatedAt              time.Time `bun:"created_at,notnull" json:"created_at"`
	UpdatedAt              time.Time `bun:"updated_at,notnull" json:"updated_at"`
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
	// SourceManifestChecksum is internal provenance set by ImportFromGitHub; it
	// is not client-settable (json:"-") so REST callers cannot forge it.
	SourceManifestChecksum string `json:"-"`
	// Origin records the blueprint's provenance ("builtin", "user", "import").
	// It is server-derived and never client-settable (json:"-"): HTTP create
	// paths are always user-authored; ImportFromGitHub sets "import" and the
	// built-in seeder writes "builtin" directly, both internal.
	Origin string `json:"-"`
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
// with its blueprint metadata and (when present) its GitHub source provenance
// and latest update-check result. Returned by GET /api/blueprints/applied.
type AppliedBlueprint struct {
	BlueprintID     string     `json:"blueprintId"`
	Name            string     `json:"name"`
	Version         string     `json:"version"`
	Description     string     `json:"description,omitempty"`
	Author          string     `json:"author,omitempty"`
	Checksum        string     `json:"checksum"`
	AppliedAt       time.Time  `json:"appliedAt"`
	SourceURL       string     `json:"sourceUrl,omitempty"`
	SourceRef       string     `json:"sourceRef,omitempty"`
	UpdateAvailable bool       `json:"updateAvailable"`
	LatestVersion   string     `json:"latestVersion,omitempty"`
	LastCheckedAt   *time.Time `json:"lastCheckedAt,omitempty"`
	Origin          string     `json:"origin"`
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

// Update-check job actions and statuses.
const (
	UpdateActionCheck  = "check"
	UpdateActionUpdate = "update"

	UpdateJobStatusPending   = "pending"
	UpdateJobStatusRunning   = "running"
	UpdateJobStatusSucceeded = "succeeded"
	UpdateJobStatusFailed    = "failed"
)

// BlueprintUpdateCheck is the latest update-check result for one applied,
// GitHub-imported blueprint in a project. One row per (project_id,
// blueprint_name); upserted on every check. Table: kb.blueprint_update_checks.
type BlueprintUpdateCheck struct {
	bun.BaseModel `bun:"table:kb.blueprint_update_checks,alias:buc"`

	ID              string     `bun:"id,pk,type:uuid,default:uuid_generate_v4()" json:"id"`
	ProjectID       string     `bun:"project_id,notnull,type:uuid" json:"projectId"`
	BlueprintName   string     `bun:"blueprint_name,notnull" json:"blueprintName"`
	SourceURL       string     `bun:"source_url,type:text" json:"sourceUrl,omitempty"`
	SourceRef       string     `bun:"source_ref,type:text" json:"sourceRef,omitempty"`
	LatestVersion   string     `bun:"latest_version,type:text" json:"latestVersion,omitempty"`
	LatestChecksum  string     `bun:"latest_checksum,type:text" json:"latestChecksum,omitempty"`
	UpdateAvailable bool       `bun:"update_available,notnull,default:false" json:"updateAvailable"`
	LastCheckedAt   *time.Time `bun:"last_checked_at,type:timestamptz" json:"lastCheckedAt,omitempty"`
	LastCheckError  string     `bun:"last_check_error,type:text" json:"lastCheckError,omitempty"`
	CreatedAt       time.Time  `bun:"created_at,notnull,default:now()" json:"createdAt"`
	UpdatedAt       time.Time  `bun:"updated_at,notnull,default:now()" json:"updatedAt"`
}

// BlueprintUpdateJob is one durable, retryable update-check (or future update)
// job. Claimed by the job runner with FOR UPDATE SKIP LOCKED. Table:
// kb.blueprint_update_jobs.
type BlueprintUpdateJob struct {
	bun.BaseModel `bun:"table:kb.blueprint_update_jobs,alias:buj"`

	ID            string          `bun:"id,pk,type:uuid,default:uuid_generate_v4()" json:"id"`
	ProjectID     string          `bun:"project_id,notnull,type:uuid" json:"projectId"`
	BlueprintName string          `bun:"blueprint_name,notnull" json:"blueprintName"`
	Action        string          `bun:"action,notnull" json:"action"`
	Status        string          `bun:"status,notnull,default:'pending'" json:"status"`
	Attempts      int             `bun:"attempts,notnull,default:0" json:"attempts"`
	LastError     string          `bun:"last_error,type:text" json:"lastError,omitempty"`
	Result        json.RawMessage `bun:"result,type:jsonb,notnull,default:'{}'" json:"result,omitempty"`
	CreatedAt     time.Time       `bun:"created_at,notnull,default:now()" json:"createdAt"`
	StartedAt     *time.Time      `bun:"started_at,type:timestamptz" json:"startedAt,omitempty"`
	FinishedAt    *time.Time      `bun:"finished_at,type:timestamptz" json:"finishedAt,omitempty"`
}

// GithubCredential stores a project's encrypted GitHub personal access token.
// The token is never serialized or logged; the plaintext is decrypted only for
// the outbound codeload fetch. Table: kb.github_credentials.
type GithubCredential struct {
	bun.BaseModel `bun:"table:kb.github_credentials,alias:gc"`

	ProjectID      string    `bun:"project_id,pk,type:uuid" json:"-"`
	EncryptedToken []byte    `bun:"encrypted_token,type:bytea,notnull" json:"-"`
	Nonce          []byte    `bun:"nonce,type:bytea,notnull" json:"-"`
	CreatedAt      time.Time `bun:"created_at,notnull,default:now()" json:"-"`
	UpdatedAt      time.Time `bun:"updated_at,notnull,default:now()" json:"-"`
}

// BlueprintUpdateStatus is the response of GET /api/blueprints/:name/updates:
// the latest check result (flattened) plus the latest update job, when one has
// been enqueued. The embedded check pointer is nil when no check has run yet.
type BlueprintUpdateStatus struct {
	*BlueprintUpdateCheck
	LatestJob *BlueprintUpdateJob `json:"latestJob,omitempty"`
}
