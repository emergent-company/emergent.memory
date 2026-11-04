package discoveryjobs

import (
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

// DiscoveryJob represents a schema discovery job
type DiscoveryJob struct {
	bun.BaseModel `bun:"table:kb.discovery_jobs,alias:dj"`

	ID                      uuid.UUID  `bun:"id,pk,type:uuid,default:gen_random_uuid()" json:"id"`
	OrganizationID          uuid.UUID  `bun:"organization_id,type:uuid,notnull" json:"organizationId"`
	ProjectID               uuid.UUID  `bun:"project_id,type:uuid,notnull" json:"projectId"`
	Status                  string     `bun:"status,notnull" json:"status"`
	Progress                JSONMap    `bun:"progress,type:jsonb,notnull,default:'{}'::jsonb" json:"progress"`
	Config                  JSONMap    `bun:"config,type:jsonb,notnull,default:'{}'::jsonb" json:"config"`
	KBPurpose               string     `bun:"kb_purpose,notnull" json:"kbPurpose"`
	DiscoveredTypes         JSONArray  `bun:"discovered_types,type:jsonb,default:'[]'::jsonb" json:"discoveredTypes"`
	DiscoveredRelationships JSONArray  `bun:"discovered_relationships,type:jsonb,default:'[]'::jsonb" json:"discoveredRelationships"`
	SchemaID                *uuid.UUID `bun:"schema_id,type:uuid" json:"schemaId,omitempty"`
	ErrorMessage            *string    `bun:"error_message" json:"errorMessage,omitempty"`
	RetryCount              int        `bun:"retry_count,default:0" json:"retryCount"`
	CreatedAt               time.Time  `bun:"created_at,notnull,default:now()" json:"createdAt"`
	StartedAt               *time.Time `bun:"started_at" json:"startedAt,omitempty"`
	CompletedAt             *time.Time `bun:"completed_at" json:"completedAt,omitempty"`
	UpdatedAt               time.Time  `bun:"updated_at,notnull,default:now()" json:"updatedAt"`
}

// DiscoveryTypeCandidate represents a discovered type candidate
type DiscoveryTypeCandidate struct {
	bun.BaseModel `bun:"table:kb.discovery_type_candidates,alias:dtc"`

	ID                    uuid.UUID   `bun:"id,pk,type:uuid,default:gen_random_uuid()" json:"id"`
	JobID                 uuid.UUID   `bun:"job_id,type:uuid,notnull" json:"jobId"`
	BatchNumber           int         `bun:"batch_number,notnull" json:"batchNumber"`
	TypeName              string      `bun:"type_name,notnull" json:"typeName"`
	Description           *string     `bun:"description" json:"description,omitempty"`
	Confidence            float32     `bun:"confidence,notnull" json:"confidence"`
	InferredSchema        JSONMap     `bun:"inferred_schema,type:jsonb,notnull" json:"inferredSchema"`
	ExampleInstances      JSONArray   `bun:"example_instances,type:jsonb,default:'[]'::jsonb" json:"exampleInstances"`
	Frequency             int         `bun:"frequency,default:1" json:"frequency"`
	ProposedRelationships JSONArray   `bun:"proposed_relationships,type:jsonb,default:'[]'::jsonb" json:"proposedRelationships"`
	SourceDocumentIDs     []uuid.UUID `bun:"source_document_ids,type:uuid[],default:'{}'::uuid[]" json:"sourceDocumentIds"`
	ExtractionContext     *string     `bun:"extraction_context" json:"extractionContext,omitempty"`
	RefinementIteration   int         `bun:"refinement_iteration,default:1" json:"refinementIteration"`
	MergedFrom            []uuid.UUID `bun:"merged_from,type:uuid[]" json:"mergedFrom,omitempty"`
	Status                string      `bun:"status,notnull,default:'candidate'" json:"status"`
	CreatedAt             time.Time   `bun:"created_at,notnull,default:now()" json:"createdAt"`
	UpdatedAt             time.Time   `bun:"updated_at,notnull,default:now()" json:"updatedAt"`
}

// JSONMap is a helper type for JSONB map fields
type JSONMap map[string]any

// JSONArray is a helper type for JSONB array fields
type JSONArray []any

// Valid job statuses
const (
	StatusPending            = "pending"
	StatusAnalyzingDocuments = "analyzing_documents"
	StatusExtractingTypes    = "extracting_types"
	StatusRefiningSchemas    = "refining_schemas"
	StatusCreatingPack       = "creating_pack"
	StatusCompleted          = "completed"
	StatusFailed             = "failed"
	StatusCancelled          = "cancelled"
)

// AllStatuses lists every status a discovery job can hold, in lifecycle order.
// The terminal/cancellable predicates and the repository's statement predicates
// are derived from this list, so a new status cannot leave them disagreeing.
func AllStatuses() []string {
	out := make([]string, len(allStatuses))
	copy(out, allStatuses)
	return out
}

var allStatuses = []string{
	StatusPending,
	StatusAnalyzingDocuments,
	StatusExtractingTypes,
	StatusRefiningSchemas,
	StatusCreatingPack,
	StatusCompleted,
	StatusFailed,
	StatusCancelled,
}

// IsTerminalStatus reports whether a discovery job has finished running. A
// terminal job can be retried (which creates a new job) but never advanced.
func IsTerminalStatus(status string) bool {
	return status == StatusCompleted || status == StatusFailed || status == StatusCancelled
}

// IsCancellableStatus reports whether a member may cancel a job in this status.
// A run that is creating its pack is excluded: the pack is being written and
// the run finishes instead, so a cancel cannot orphan a half-written pack.
func IsCancellableStatus(status string) bool {
	return status == StatusPending ||
		status == StatusAnalyzingDocuments ||
		status == StatusExtractingTypes ||
		status == StatusRefiningSchemas
}

// Valid candidate statuses
const (
	CandidateStatusCandidate = "candidate"
	CandidateStatusApproved  = "approved"
	CandidateStatusRejected  = "rejected"
	CandidateStatusMerged    = "merged"
)

// IsTerminal reports whether the job has finished running.
func (j *DiscoveryJob) IsTerminal() bool {
	return IsTerminalStatus(j.Status)
}

// IsCancellable reports whether a member may cancel the job.
func (j *DiscoveryJob) IsCancellable() bool {
	return IsCancellableStatus(j.Status)
}
