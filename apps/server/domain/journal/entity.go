package journal

import (
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

// Event types for journal entries.
const (
	EventTypeCreated  = "created"
	EventTypeUpdated  = "updated"
	EventTypeDeleted  = "deleted"
	EventTypeRestored = "restored"
	EventTypeRelated  = "related"
	EventTypeBatch    = "batch"
	EventTypeMerge    = "merge"
	EventTypeNote     = "note"
)

// Entity types stored in the journal.
const (
	EntityObject       = "graph_object"
	EntityRelationship = "graph_relationship"
)

// Actor types.
const (
	ActorUser   = "user"
	ActorAgent  = "agent"
	ActorSystem = "system"
)

// JournalEntry is a row in kb.project_journal.
type JournalEntry struct {
	bun.BaseModel `bun:"table:kb.project_journal,alias:je"`

	ID         uuid.UUID      `bun:"id,pk,type:uuid,default:gen_random_uuid()" json:"id"`
	ProjectID  uuid.UUID      `bun:"project_id,type:uuid,notnull" json:"project_id"`
	BranchID   *uuid.UUID     `bun:"branch_id,type:uuid" json:"branch_id,omitempty"`
	EventType  string         `bun:"event_type,notnull" json:"event_type"`
	EntityType *string        `bun:"entity_type" json:"entity_type,omitempty"`
	EntityID   *uuid.UUID     `bun:"entity_id,type:uuid" json:"entity_id,omitempty"`
	ObjectType *string        `bun:"object_type" json:"object_type,omitempty"`
	ActorType  string         `bun:"actor_type,notnull" json:"actor_type"`
	ActorID    *uuid.UUID     `bun:"actor_id,type:uuid" json:"actor_id,omitempty"`
	Metadata   map[string]any `bun:"metadata,type:jsonb,notnull" json:"metadata"`
	CreatedAt  time.Time      `bun:"created_at,notnull" json:"created_at"`

	Notes []*JournalNote `bun:"-" json:"notes,omitempty"`
}

// JournalNote is a row in kb.project_journal_notes.
type JournalNote struct {
	bun.BaseModel `bun:"table:kb.project_journal_notes,alias:jn"`

	ID        uuid.UUID  `bun:"id,pk,type:uuid,default:gen_random_uuid()" json:"id"`
	ProjectID uuid.UUID  `bun:"project_id,type:uuid,notnull" json:"project_id"`
	BranchID  *uuid.UUID `bun:"branch_id,type:uuid" json:"branch_id,omitempty"`
	JournalID *uuid.UUID `bun:"journal_id,type:uuid" json:"journal_id,omitempty"`
	Body      string     `bun:"body,notnull" json:"body"`
	ActorType string     `bun:"actor_type,notnull" json:"actor_type"`
	ActorID   *uuid.UUID `bun:"actor_id,type:uuid" json:"actor_id,omitempty"`
	CreatedAt time.Time  `bun:"created_at,notnull" json:"created_at"`
}

// LogParams are passed from callers (e.g. graph service) to log a mutation.
type LogParams struct {
	ProjectID  uuid.UUID
	BranchID   *uuid.UUID
	EventType  string
	EntityType *string
	EntityID   *uuid.UUID
	ObjectType *string
	ActorType  string
	ActorID    *uuid.UUID
	Metadata   map[string]any
}

// ListParams controls querying of journal entries.
type ListParams struct {
	ProjectID uuid.UUID
	// BranchID filters to entries from a specific branch.
	// nil = main branch only (branch_id IS NULL).
	// A non-nil pointer with a zero value is never produced; callers always
	// set an explicit UUID when filtering by a named branch.
	BranchID *uuid.UUID
	Since    *time.Time
	Limit    int
	Page     int
}

// AddNoteRequest is the payload for adding a note.
type AddNoteRequest struct {
	Body      string     `json:"body"`
	BranchID  *uuid.UUID `json:"branch_id,omitempty"`
	JournalID *uuid.UUID `json:"journal_id,omitempty"`
	ActorType string     `json:"actor_type,omitempty"`
	ActorID   *uuid.UUID `json:"actor_id,omitempty"`
}

// JournalResponse is the API response for listing.
type JournalResponse struct {
	Entries []*JournalEntry `json:"entries"`
	Notes   []*JournalNote  `json:"notes"` // standalone notes (journal_id IS NULL)
	Total   int             `json:"total"`
}
