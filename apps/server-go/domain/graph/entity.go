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

	Type   string  `bun:"type,notnull" json:"type"`
	Key    *string `bun:"key" json:"key,omitempty"`
	Status *string `bun:"status" json:"status,omitempty"`

	Properties    map[string]any `bun:"properties,type:jsonb,notnull,default:'{}'" json:"properties"`
	Labels        []string       `bun:"labels,array,notnull,default:'{}'" json:"labels"`
	ChangeSummary map[string]any `bun:"change_summary,type:jsonb" json:"change_summary,omitempty"`
	ContentHash   []byte         `bun:"content_hash,type:bytea" json:"-"`

	// Timestamps
	CreatedAt      time.Time  `bun:"created_at,notnull,default:now()" json:"created_at"`
	UpdatedAt      time.Time  `bun:"updated_at,notnull,default:now()" json:"updated_at"`
	DeletedAt      *time.Time `bun:"deleted_at" json:"deleted_at,omitempty"`
	LastAccessedAt *time.Time `bun:"last_accessed_at,type:timestamptz" json:"last_accessed_at,omitempty"`

	// Full-text search vector (generated)
	FTS *string `bun:"fts,type:tsvector" json:"-"`

	// Embedding fields
	EmbeddingUpdatedAt *time.Time `bun:"embedding_updated_at" json:"-"`
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
}

// GraphRelationship represents a versioned edge between two graph objects.
type GraphRelationship struct {
	bun.BaseModel `bun:"table:kb.graph_relationships,alias:gr"`

	ID           uuid.UUID  `bun:"id,pk,type:uuid,default:uuid_generate_v4()" json:"id"`
	ProjectID    uuid.UUID  `bun:"project_id,type:uuid,notnull" json:"project_id"`
	BranchID     *uuid.UUID `bun:"branch_id,type:uuid" json:"branch_id,omitempty"`
	CanonicalID  uuid.UUID  `bun:"canonical_id,type:uuid,notnull" json:"canonical_id"`
	SupersedesID *uuid.UUID `bun:"supersedes_id,type:uuid" json:"supersedes_id,omitempty"`
	Version      int        `bun:"version,notnull,default:1" json:"version"`

	Type  string    `bun:"type,notnull" json:"type"`
	SrcID uuid.UUID `bun:"src_id,type:uuid,notnull" json:"src_id"`
	DstID uuid.UUID `bun:"dst_id,type:uuid,notnull" json:"dst_id"`

	Properties    map[string]any `bun:"properties,type:jsonb,notnull,default:'{}'" json:"properties"`
	Weight        *float32       `bun:"weight" json:"weight,omitempty"`
	ChangeSummary map[string]any `bun:"change_summary,type:jsonb" json:"change_summary,omitempty"`
	ContentHash   []byte         `bun:"content_hash,type:bytea" json:"-"`

	// Temporal validity
	ValidFrom *time.Time `bun:"valid_from" json:"valid_from,omitempty"`
	ValidTo   *time.Time `bun:"valid_to" json:"valid_to,omitempty"`

	// Timestamps
	CreatedAt time.Time  `bun:"created_at,notnull,default:now()" json:"created_at"`
	DeletedAt *time.Time `bun:"deleted_at" json:"deleted_at,omitempty"`

	// Populated by joins
	SrcObject *GraphObject `bun:"rel:belongs-to,join:src_id=id" json:"src,omitempty"`
	DstObject *GraphObject `bun:"rel:belongs-to,join:dst_id=id" json:"dst,omitempty"`
}

// Branch represents a named branch for isolated changes.
type Branch struct {
	bun.BaseModel `bun:"table:kb.branches,alias:b"`

	ID             uuid.UUID  `bun:"id,pk,type:uuid,default:uuid_generate_v4()" json:"id"`
	ProjectID      uuid.UUID  `bun:"project_id,type:uuid,notnull" json:"project_id"`
	Name           string     `bun:"name,notnull" json:"name"`
	ParentBranchID *uuid.UUID `bun:"parent_branch_id,type:uuid" json:"parent_branch_id,omitempty"`
	CreatedAt      time.Time  `bun:"created_at,notnull,default:now()" json:"created_at"`
}

// BranchLineage stores the transitive closure of branch ancestry.
type BranchLineage struct {
	bun.BaseModel `bun:"table:kb.branch_lineage,alias:bl"`

	BranchID         uuid.UUID `bun:"branch_id,pk,type:uuid" json:"branch_id"`
	AncestorBranchID uuid.UUID `bun:"ancestor_branch_id,pk,type:uuid" json:"ancestor_branch_id"`
	Depth            int       `bun:"depth,notnull" json:"depth"`
}
