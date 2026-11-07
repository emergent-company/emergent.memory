package discoveryjobs

import (
	"time"

	"github.com/google/uuid"
)

// Discovery scopes accepted by StartDiscovery.
const (
	// DiscoveryScopeDocuments analyses the document_ids named in the request.
	DiscoveryScopeDocuments = "documents"
	// DiscoveryScopeProject analyses the project's own documents, newest first,
	// up to the per-run cap. It is what the UI's "all documents" action uses.
	DiscoveryScopeProject = "project"
)

// Discovery triggers recorded in a job's config so an operator can tell an
// automatic run from an explicit one.
const (
	DiscoveryTriggerManual     = "manual"
	DiscoveryTriggerSourceSync = "source_sync"
)

// StartDiscoveryRequest is the request body for starting a discovery job
type StartDiscoveryRequest struct {
	// DocumentIDs names the documents to analyse. Required when Scope is empty
	// or DiscoveryScopeDocuments; ignored when Scope is DiscoveryScopeProject.
	DocumentIDs []uuid.UUID `json:"document_ids" validate:"omitempty,dive,uuid"`
	// Scope selects what to analyse: DiscoveryScopeDocuments (default) or
	// DiscoveryScopeProject.
	Scope string `json:"scope" validate:"omitempty,oneof=documents project"`
	// Limit caps a DiscoveryScopeProject run. Zero means the server default;
	// a value above the server cap is clamped to it.
	Limit         int     `json:"limit" validate:"omitempty,min=1"`
	BatchSize     int     `json:"batch_size" validate:"min=1,max=50"`
	MinConfidence float32 `json:"min_confidence" validate:"min=0,max=1"`
	// IncludeRelationships controls whether the run also proposes relationship
	// types. It is a pointer so an omitted field means "yes": a document that
	// names entities also relates them, and clients that never sent the field
	// (the web UI's document and project discovery actions) produced packs with
	// zero relationship types (#234). An explicit `false` opts out.
	IncludeRelationships *bool `json:"include_relationships"`
	MaxIterations        int   `json:"max_iterations" validate:"min=1,max=10"`
	// Trigger is resolved server-side (manual | source_sync) and is never read
	// from the request body; it records why the job exists.
	Trigger string `json:"-"`
}

// StartDiscoveryResponse is the response for starting a discovery job
type StartDiscoveryResponse struct {
	JobID uuid.UUID `json:"job_id"`
	// DocumentCount is how many documents this job will analyse.
	DocumentCount int `json:"document_count"`
	// TotalDocuments is how many head documents the project had when the run
	// started. It is larger than DocumentCount when the per-run cap applied, so
	// a caller can tell the user the run covered a subset.
	TotalDocuments int `json:"total_documents"`
}

// RetryDiscoveryRequest is the (optional) body for re-running a terminal job.
// It clones the original job's document set and only allows a batch-size
// override, so a retry can never silently widen the scope of the original run.
type RetryDiscoveryRequest struct {
	BatchSize int `json:"batch_size" validate:"omitempty,min=1,max=50"`
}

// JobStatusResponse is the response for getting job status
type JobStatusResponse struct {
	ID                      uuid.UUID  `json:"id"`
	Status                  string     `json:"status"`
	Progress                JSONMap    `json:"progress"`
	CreatedAt               time.Time  `json:"created_at"`
	StartedAt               *time.Time `json:"started_at,omitempty"`
	CompletedAt             *time.Time `json:"completed_at,omitempty"`
	ErrorMessage            *string    `json:"error_message,omitempty"`
	DiscoveredTypes         JSONArray  `json:"discovered_types"`
	DiscoveredRelationships JSONArray  `json:"discovered_relationships"`
	SchemaID                *uuid.UUID `json:"schema_id,omitempty"`
}

// JobListItem is a summary of a discovery job for listing
type JobListItem struct {
	ID                      uuid.UUID  `json:"id"`
	Status                  string     `json:"status"`
	Progress                JSONMap    `json:"progress"`
	CreatedAt               time.Time  `json:"created_at"`
	CompletedAt             *time.Time `json:"completed_at,omitempty"`
	ErrorMessage            *string    `json:"error_message,omitempty"`
	DiscoveredTypes         JSONArray  `json:"discovered_types"`
	DiscoveredRelationships JSONArray  `json:"discovered_relationships"`
	SchemaID                *uuid.UUID `json:"schema_id,omitempty"`
	// Trigger/scope/document counts describe what the run covered, so a caller
	// can distinguish an automatic sync run from an explicit one and tell the
	// user when the per-run cap limited a project-wide run.
	Trigger        string `json:"trigger,omitempty"`
	DocumentCount  int    `json:"document_count,omitempty"`
	TotalDocuments int    `json:"total_documents,omitempty"`
	// Cancellable is the server's answer to "may this job be cancelled now?".
	// A client renders the cancel action from it instead of re-deriving the
	// status set, so the card and the API cannot drift apart.
	Cancellable bool `json:"cancellable"`
}

// CancelJobResponse is the response for cancelling a job
type CancelJobResponse struct {
	Message string `json:"message"`
}

// FinalizeDiscoveryRequest is the request body for finalizing discovery
type FinalizeDiscoveryRequest struct {
	PackName string `json:"packName"`
	Mode     string `json:"mode" validate:"required,oneof=create extend enrich create_rich create_rich_combined create_rich_sequential"`
	// DocumentID is used by enrich and create_rich modes to load document text
	// for server-side schema property generation.
	DocumentID            string                 `json:"documentId,omitempty"`
	ExistingPackID        *uuid.UUID             `json:"existingPackId,omitempty"`
	IncludedTypes         []IncludedType         `json:"includedTypes"`
	IncludedRelationships []IncludedRelationship `json:"includedRelationships"`
}

// IncludedType represents a type selected for the schema
type IncludedType struct {
	TypeName           string         `json:"type_name"`
	Description        string         `json:"description"`
	Properties         map[string]any `json:"properties"`
	RequiredProperties []string       `json:"required_properties"`
	ExampleInstances   []any          `json:"example_instances"`
	Frequency          int            `json:"frequency"`
}

// IncludedRelationship represents a relationship selected for the schema
type IncludedRelationship struct {
	SourceType   string `json:"source_type"`
	TargetType   string `json:"target_type"`
	RelationType string `json:"relation_type"`
	Description  string `json:"description"`
	Cardinality  string `json:"cardinality"`
}

// FinalizeDiscoveryResponse is the response for finalizing discovery
type FinalizeDiscoveryResponse struct {
	SchemaID uuid.UUID `json:"schema_id"`
	Message  string    `json:"message"`
}

// DiscoveredType represents a type discovered by the LLM
type DiscoveredType struct {
	TypeName           string         `json:"type_name"`
	Description        string         `json:"description"`
	Confidence         float32        `json:"confidence"`
	Properties         map[string]any `json:"properties"`
	RequiredProperties []string       `json:"required_properties"`
	ExampleInstances   []any          `json:"example_instances"`
	Frequency          int            `json:"frequency"`
}

// DiscoveredRelationship represents a relationship discovered by the LLM
type DiscoveredRelationship struct {
	SourceType   string  `json:"source_type"`
	TargetType   string  `json:"target_type"`
	RelationType string  `json:"relation_type"`
	Description  string  `json:"description"`
	Confidence   float32 `json:"confidence"`
	Cardinality  string  `json:"cardinality"`
}

// JobConfig represents the configuration stored in the job
type JobConfig struct {
	DocumentIDs          []string `json:"document_ids"`
	BatchSize            int      `json:"batch_size"`
	MinConfidence        float32  `json:"min_confidence"`
	IncludeRelationships bool     `json:"include_relationships"`
	MaxIterations        int      `json:"max_iterations"`
	// Scope is the scope the job was started with (documents | project).
	Scope string `json:"scope,omitempty"`
	// Trigger records why the job exists: manual or source_sync.
	Trigger string `json:"trigger,omitempty"`
	// DocumentCount and TotalDocuments record whether the per-run cap applied.
	DocumentCount  int `json:"document_count,omitempty"`
	TotalDocuments int `json:"total_documents,omitempty"`
}

// JobProgress represents the progress of a discovery job
type JobProgress struct {
	CurrentStep int    `json:"current_step"`
	TotalSteps  int    `json:"total_steps"`
	Message     string `json:"message"`
}
