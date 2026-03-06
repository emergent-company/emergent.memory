package discoveryjobs

import (
	"time"

	"github.com/google/uuid"
)

// StartDiscoveryRequest is the request body for starting a discovery job
type StartDiscoveryRequest struct {
	DocumentIDs          []uuid.UUID `json:"document_ids" validate:"required,min=1"`
	BatchSize            int         `json:"batch_size" validate:"min=1,max=50"`
	MinConfidence        float32     `json:"min_confidence" validate:"min=0,max=1"`
	IncludeRelationships bool        `json:"include_relationships"`
	MaxIterations        int         `json:"max_iterations" validate:"min=1,max=10"`
}

// StartDiscoveryResponse is the response for starting a discovery job
type StartDiscoveryResponse struct {
	JobID uuid.UUID `json:"job_id"`
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
	TemplatePackID          *uuid.UUID `json:"template_pack_id,omitempty"`
}

// JobListItem is a summary of a discovery job for listing
type JobListItem struct {
	ID                      uuid.UUID  `json:"id"`
	Status                  string     `json:"status"`
	Progress                JSONMap    `json:"progress"`
	CreatedAt               time.Time  `json:"created_at"`
	CompletedAt             *time.Time `json:"completed_at,omitempty"`
	DiscoveredTypes         JSONArray  `json:"discovered_types"`
	DiscoveredRelationships JSONArray  `json:"discovered_relationships"`
	TemplatePackID          *uuid.UUID `json:"template_pack_id,omitempty"`
}

// CancelJobResponse is the response for cancelling a job
type CancelJobResponse struct {
	Message string `json:"message"`
}

// FinalizeDiscoveryRequest is the request body for finalizing discovery
type FinalizeDiscoveryRequest struct {
	PackName              string                 `json:"packName" validate:"required"`
	Mode                  string                 `json:"mode" validate:"required,oneof=create extend"`
	ExistingPackID        *uuid.UUID             `json:"existingPackId,omitempty"`
	IncludedTypes         []IncludedType         `json:"includedTypes" validate:"required,min=1"`
	IncludedRelationships []IncludedRelationship `json:"includedRelationships"`
}

// IncludedType represents a type selected for the template pack
type IncludedType struct {
	TypeName           string         `json:"type_name"`
	Description        string         `json:"description"`
	Properties         map[string]any `json:"properties"`
	RequiredProperties []string       `json:"required_properties"`
	ExampleInstances   []any          `json:"example_instances"`
	Frequency          int            `json:"frequency"`
}

// IncludedRelationship represents a relationship selected for the template pack
type IncludedRelationship struct {
	SourceType   string `json:"source_type"`
	TargetType   string `json:"target_type"`
	RelationType string `json:"relation_type"`
	Description  string `json:"description"`
	Cardinality  string `json:"cardinality"`
}

// FinalizeDiscoveryResponse is the response for finalizing discovery
type FinalizeDiscoveryResponse struct {
	TemplatePackID uuid.UUID `json:"template_pack_id"`
	Message        string    `json:"message"`
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
}

// JobProgress represents the progress of a discovery job
type JobProgress struct {
	CurrentStep int    `json:"current_step"`
	TotalSteps  int    `json:"total_steps"`
	Message     string `json:"message"`
}
