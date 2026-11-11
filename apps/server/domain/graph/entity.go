package graph

import (
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

// GraphObject represents a versioned knowledge graph node.
// Each modification creates a new row (version) linked via canonical_id.
type GraphObject struct {
	bun.BaseModel `bun:"table:kb.graph_objects,alias:go"`

	ID           uuid.UUID  `bun:"id,pk,type:uuid,default:uuid_generate_v4()" json:"id"`
	OrgID        *string    `bun:"-" json:"org_id,omitempty"` // Column removed in Phase 5, kept for API compatibility
	ProjectID    uuid.UUID  `bun:"project_id,type:uuid,notnull" json:"project_id"`
	BranchID     *uuid.UUID `bun:"branch_id,type:uuid" json:"branch_id,omitempty"`
	CanonicalID  uuid.UUID  `bun:"canonical_id,type:uuid,notnull" json:"canonical_id"`
	SupersedesID *uuid.UUID `bun:"supersedes_id,type:uuid" json:"supersedes_id,omitempty"`
	Version      int        `bun:"version,notnull,default:1" json:"version"`

	// MergedToCanonicalID points to the target-branch canonical_id this object
	// was cloned to during a merge (the merge ledger). Non-nil on source-branch
	// objects means "already merged"; used by compare/diff to distinguish merged
	// from pending.
	MergedToCanonicalID *uuid.UUID `bun:"merged_to_canonical_id,type:uuid" json:"merged_to_canonical_id,omitempty"`

	Type string `bun:"type,notnull" json:"type"`
	// Name is the first-class human title. Key stays the internal dedup/upsert
	// identity and is not the user-facing label.
	Name   *string `bun:"name" json:"name,omitempty"`
	Key    *string `bun:"key" json:"key,omitempty"`
	Status *string `bun:"status" json:"status,omitempty"`
	// Assignee is the work lane for board-enabled types (the agent identity
	// expected to claim the object). NULL = any listening agent.
	Assignee  *string `bun:"assignee" json:"assignee,omitempty"`
	Namespace *string `bun:"namespace" json:"namespace,omitempty"`

	Properties    map[string]any `bun:"properties,type:jsonb,notnull,default:'{}'" json:"properties"`
	Labels        []string       `bun:"labels,array,notnull,default:'{}'" json:"labels"`
	ChangeSummary map[string]any `bun:"change_summary,type:jsonb" json:"change_summary,omitempty"`
	ContentHash   []byte         `bun:"content_hash,type:bytea" json:"-"`

	// Timestamps
	CreatedAt      time.Time  `bun:"created_at,notnull,default:now()" json:"created_at"`
	UpdatedAt      time.Time  `bun:"updated_at,notnull,default:now()" json:"updated_at"`
	DeletedAt      *time.Time `bun:"deleted_at" json:"deleted_at,omitempty"`
	DeleteReason   *string    `bun:"delete_reason" json:"delete_reason,omitempty"`
	LastAccessedAt *time.Time `bun:"last_accessed_at,type:timestamptz" json:"last_accessed_at,omitempty"`

	// Full-text search vector (generated)
	FTS *string `bun:"fts,type:tsvector" json:"-"`

	// Embedding fields
	EmbeddingUpdatedAt *time.Time `bun:"embedding_updated_at" json:"embedding_updated_at,omitempty"`
	// Note: embedding_v2 is vector(768), handled via raw SQL for pgvector queries

	// Extraction metadata
	ExtractionJobID      *uuid.UUID `bun:"extraction_job_id,type:uuid" json:"extraction_job_id,omitempty"`
	ExtractionConfidence *float32   `bun:"extraction_confidence" json:"extraction_confidence,omitempty"`
	NeedsReview          *bool      `bun:"needs_review,default:false" json:"needs_review,omitempty"`
	ReviewedBy           *uuid.UUID `bun:"reviewed_by,type:uuid" json:"reviewed_by,omitempty"`
	ReviewedAt           *time.Time `bun:"reviewed_at" json:"reviewed_at,omitempty"`

	// Actor tracking
	ActorType *string    `bun:"actor_type,default:'user'" json:"actor_type,omitempty"`
	ActorID   *uuid.UUID `bun:"actor_id,type:uuid" json:"actor_id,omitempty"`

	// Write stamp (slice 1 of #2376): the credential and agent run that produced
	// this version, the class of data it is, and what the writer observed.
	//
	// These are columns, never `properties`: computeContentHash hashes
	// properties/status/key/labels, so a provenance property would flip the
	// content hash on a metadata-only write and defeat
	// carryEmbeddingIfUnchanged (#1478).
	//
	// CredentialKind is never empty; an unattributable write stores "none" so it
	// is diagnosable rather than absent.
	CredentialKind string     `bun:"credential_kind,notnull,default:'none'" json:"credential_kind"`
	CredentialID   *uuid.UUID `bun:"credential_id,type:uuid" json:"credential_id,omitempty"`
	RunID          *uuid.UUID `bun:"run_id,type:uuid" json:"run_id,omitempty"`
	RootRunID      *uuid.UUID `bun:"root_run_id,type:uuid" json:"root_run_id,omitempty"`
	// DataClass is source | synthesized | adopted, relative to external sources.
	// Everything Memory owns today is synthesized; a source push sets "source"
	// together with OwnerSourceID (slice 2/3).
	DataClass string `bun:"data_class,notnull,default:'synthesized'" json:"data_class"`
	// OwnerSourceID is the kb.sources row that owns this object. Present only
	// when DataClass = "source".
	OwnerSourceID *uuid.UUID `bun:"owner_source_id,type:uuid" json:"owner_source_id,omitempty"`
	// InputHash is the content hash of the row this version supersedes, or nil
	// when it supersedes nothing (a create). It is the writer's view of the
	// content before the write, so an edit made against an older version is
	// detectable by comparing the superseded row's content_hash with the value
	// recorded here — no replay of the write needed.
	InputHash *string `bun:"input_hash" json:"input_hash,omitempty"`

	// Schema version for template pack
	SchemaVersion *string `bun:"schema_version" json:"schema_version,omitempty"`

	// Migration archive - preserves dropped fields from schema migrations
	MigrationArchive []map[string]any `bun:"migration_archive,type:jsonb,default:'[]'" json:"migration_archive,omitempty"`

	// External source fields - columns removed from schema in Phase 5, kept for API compatibility
	ExternalSource    *string    `bun:"-" json:"external_source,omitempty"`
	ExternalID        *string    `bun:"-" json:"external_id,omitempty"`
	ExternalURL       *string    `bun:"-" json:"external_url,omitempty"`
	ExternalParentID  *string    `bun:"-" json:"external_parent_id,omitempty"`
	SyncedAt          *time.Time `bun:"-" json:"synced_at,omitempty"`
	ExternalUpdatedAt *time.Time `bun:"-" json:"external_updated_at,omitempty"`

	// Computed fields (not stored, populated by queries)
	RevisionCount     *int `bun:"-" json:"revision_count,omitempty"`
	RelationshipCount *int `bun:"-" json:"relationship_count,omitempty"`

	// EmbeddingStatus is a computed column (not stored) derived from the
	// embedding_v2 vector and the latest kb.graph_embedding_jobs row. Values:
	// embedded | pending | processing | failed | dead_letter | missing.
	EmbeddingStatus string `bun:"embedding_status,scanonly" json:"embedding_status,omitempty"`
}

// GraphRelationship represents a versioned edge between two graph objects.
type GraphRelationship struct {
	bun.BaseModel `bun:"table:kb.graph_relationships,alias:gr"`

	ID           uuid.UUID  `bun:"id,pk,type:uuid,default:uuid_generate_v4()" json:"id"`
	ProjectID    uuid.UUID  `bun:"project_id,type:uuid,notnull" json:"project_id"`
	Namespace    *string    `bun:"namespace" json:"namespace,omitempty"`
	BranchID     *uuid.UUID `bun:"branch_id,type:uuid" json:"branch_id,omitempty"`
	CanonicalID  uuid.UUID  `bun:"canonical_id,type:uuid,notnull" json:"canonical_id"`
	SupersedesID *uuid.UUID `bun:"supersedes_id,type:uuid" json:"supersedes_id,omitempty"`
	Version      int        `bun:"version,notnull,default:1" json:"version"`

	Type  string    `bun:"type,notnull" json:"type"`
	Label *string   `bun:"label" json:"label,omitempty"`
	SrcID uuid.UUID `bun:"src_id,type:uuid,notnull" json:"src_id"`
	DstID uuid.UUID `bun:"dst_id,type:uuid,notnull" json:"dst_id"`

	Properties    map[string]any `bun:"properties,type:jsonb,notnull,default:'{}'" json:"properties"`
	Weight        *float32       `bun:"weight" json:"weight,omitempty"`
	ChangeSummary map[string]any `bun:"change_summary,type:jsonb" json:"change_summary,omitempty"`
	ContentHash   []byte         `bun:"content_hash,type:bytea" json:"-"`

	// Actor tracking
	ActorType *string    `bun:"actor_type" json:"actor_type,omitempty"`
	ActorID   *uuid.UUID `bun:"actor_id,type:uuid" json:"actor_id,omitempty"`

	// Write stamp (slice 1 of #2376). Relationships carry the credential and run
	// that produced the version; data class and source ownership arrive with the
	// changeset API (slice 3).
	CredentialKind string     `bun:"credential_kind,notnull,default:'none'" json:"credential_kind"`
	CredentialID   *uuid.UUID `bun:"credential_id,type:uuid" json:"credential_id,omitempty"`
	RunID          *uuid.UUID `bun:"run_id,type:uuid" json:"run_id,omitempty"`
	RootRunID      *uuid.UUID `bun:"root_run_id,type:uuid" json:"root_run_id,omitempty"`

	EmbeddingUpdatedAt *time.Time `bun:"embedding_updated_at" json:"-"`

	// Temporal validity
	ValidFrom *time.Time `bun:"valid_from" json:"valid_from,omitempty"`
	ValidTo   *time.Time `bun:"valid_to" json:"valid_to,omitempty"`

	// Timestamps
	CreatedAt    time.Time  `bun:"created_at,notnull,default:now()" json:"created_at"`
	DeletedAt    *time.Time `bun:"deleted_at" json:"deleted_at,omitempty"`
	DeleteReason *string    `bun:"delete_reason" json:"delete_reason,omitempty"`

	// Populated by joins — src_id/dst_id store canonical_id values
	SrcObject *GraphObject `bun:"rel:belongs-to,join:src_id=canonical_id" json:"src,omitempty"`
	DstObject *GraphObject `bun:"rel:belongs-to,join:dst_id=canonical_id" json:"dst,omitempty"`
}

// Branch represents a named branch for isolated changes.
type Branch struct {
	bun.BaseModel `bun:"table:kb.branches,alias:b"`

	ID             uuid.UUID  `bun:"id,pk,type:uuid,default:uuid_generate_v4()" json:"id"`
	ProjectID      uuid.UUID  `bun:"project_id,type:uuid,notnull" json:"project_id"`
	Name           string     `bun:"name,notnull" json:"name"`
	Description    *string    `bun:"description" json:"description,omitempty"`
	ParentBranchID *uuid.UUID `bun:"parent_branch_id,type:uuid" json:"parent_branch_id,omitempty"`
	CreatedAt      time.Time  `bun:"created_at,notnull,default:now()" json:"created_at"`
	MergedAt       *time.Time `bun:"merged_at" json:"merged_at,omitempty"`
	// Origin records why the branch exists (extraction | agent | manual |
	// system). Metadata only — see branches.Branch.Origin.
	Origin      string     `bun:"origin,notnull,default:'manual'" json:"origin"`
	OriginID    *uuid.UUID `bun:"origin_id,type:uuid" json:"origin_id,omitempty"`
	OriginLabel *string    `bun:"origin_label" json:"origin_label,omitempty"`
}

// PendingExtractionBranch is an unmerged extraction staging branch together
// with the live HEAD rows that make it part of a project's extraction backlog.
// Merged ledger rows (merged_to_canonical_id set) and superseded/deleted rows
// are excluded, so the counts describe what a default graph read still cannot
// see.
type PendingExtractionBranch struct {
	ID                uuid.UUID `bun:"id"`
	Name              string    `bun:"name"`
	OriginLabel       *string   `bun:"origin_label"`
	CreatedAt         time.Time `bun:"created_at"`
	ObjectCount       int       `bun:"object_count"`
	RelationshipCount int       `bun:"relationship_count"`
}

// BranchLineage stores the transitive closure of branch ancestry.
type BranchLineage struct {
	bun.BaseModel `bun:"table:kb.branch_lineage,alias:bl"`

	BranchID         uuid.UUID `bun:"branch_id,pk,type:uuid" json:"branch_id"`
	AncestorBranchID uuid.UUID `bun:"ancestor_branch_id,pk,type:uuid" json:"ancestor_branch_id"`
	Depth            int       `bun:"depth,notnull" json:"depth"`
}
