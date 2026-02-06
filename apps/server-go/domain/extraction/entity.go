package extraction

import (
	"encoding/json"
	"time"

	"github.com/uptrace/bun"
)

// JobStatus represents the processing status of an extraction job
type JobStatus string

const (
	JobStatusPending    JobStatus = "pending"
	JobStatusProcessing JobStatus = "processing"
	JobStatusCompleted  JobStatus = "completed"
	JobStatusFailed     JobStatus = "failed"
	JobStatusCancelled  JobStatus = "cancelled"
	JobStatusDeadLetter JobStatus = "dead_letter" // Permanently failed after max retries
)

// ------------------------------------------------------------------
// DocumentParsingJob - Parses uploaded documents (PDF, DOCX, etc.)
// ------------------------------------------------------------------

// DocumentParsingJob represents a document parsing job in kb.document_parsing_jobs.
// These jobs parse uploaded files and extract text content.
type DocumentParsingJob struct {
	bun.BaseModel `bun:"table:kb.document_parsing_jobs,alias:dpj"`

	ID              string     `bun:"id,pk,type:uuid,default:gen_random_uuid()"`
	OrganizationID  *string    `bun:"organization_id,type:uuid"`
	ProjectID       string     `bun:"project_id,notnull,type:uuid"`
	Status          JobStatus  `bun:"status,notnull,default:'pending'"`
	SourceType      string     `bun:"source_type,notnull"` // 'file_upload', 'web_page', etc.
	SourceFilename  *string    `bun:"source_filename"`     // Original filename
	MimeType        *string    `bun:"mime_type"`           // e.g., 'application/pdf'
	FileSizeBytes   *int64     `bun:"file_size_bytes"`
	StorageKey      *string    `bun:"storage_key"`                 // S3/MinIO object key
	DocumentID      *string    `bun:"document_id,type:uuid"`       // Created document ID after parsing
	ExtractionJobID *string    `bun:"extraction_job_id,type:uuid"` // Linked extraction job
	ParsedContent   *string    `bun:"parsed_content"`              // Extracted text
	Metadata        JSON       `bun:"metadata,type:jsonb,default:'{}'"`
	ErrorMessage    *string    `bun:"error_message"`
	RetryCount      int        `bun:"retry_count,default:0"`
	MaxRetries      int        `bun:"max_retries,default:3"`
	NextRetryAt     *time.Time `bun:"next_retry_at"`
	CreatedAt       time.Time  `bun:"created_at,notnull,default:now()"`
	StartedAt       *time.Time `bun:"started_at"`
	CompletedAt     *time.Time `bun:"completed_at"`
	UpdatedAt       time.Time  `bun:"updated_at,notnull,default:now()"`
}

// ------------------------------------------------------------------
// ChunkEmbeddingJob - Generates embeddings for document chunks
// ------------------------------------------------------------------

// ChunkEmbeddingJob represents a chunk embedding job in kb.chunk_embedding_jobs.
// These jobs generate vector embeddings for document chunks.
type ChunkEmbeddingJob struct {
	bun.BaseModel `bun:"table:kb.chunk_embedding_jobs,alias:cej"`

	ID           string     `bun:"id,pk,type:uuid,default:uuid_generate_v4()"`
	ChunkID      string     `bun:"chunk_id,notnull,type:uuid"` // Reference to kb.chunks
	Status       JobStatus  `bun:"status,notnull,default:'pending'"`
	AttemptCount int        `bun:"attempt_count,notnull,default:0"`
	LastError    *string    `bun:"last_error"`
	Priority     int        `bun:"priority,notnull,default:0"`
	ScheduledAt  time.Time  `bun:"scheduled_at,notnull,default:now()"`
	StartedAt    *time.Time `bun:"started_at"`
	CompletedAt  *time.Time `bun:"completed_at"`
	CreatedAt    time.Time  `bun:"created_at,notnull,default:now()"`
	UpdatedAt    time.Time  `bun:"updated_at,notnull,default:now()"`
}

// ------------------------------------------------------------------
// GraphEmbeddingJob - Generates embeddings for graph objects
// ------------------------------------------------------------------

// GraphEmbeddingJob represents a graph embedding job in kb.graph_embedding_jobs.
// These jobs generate vector embeddings for graph objects (entities).
type GraphEmbeddingJob struct {
	bun.BaseModel `bun:"table:kb.graph_embedding_jobs,alias:gej"`

	ID           string     `bun:"id,pk,type:uuid,default:uuid_generate_v4()"`
	ObjectID     string     `bun:"object_id,notnull,type:uuid"` // Reference to kb.graph_objects
	Status       JobStatus  `bun:"status,notnull"`
	AttemptCount int        `bun:"attempt_count,notnull,default:0"`
	LastError    *string    `bun:"last_error"`
	Priority     int        `bun:"priority,notnull,default:0"`
	ScheduledAt  time.Time  `bun:"scheduled_at,notnull,default:now()"`
	StartedAt    *time.Time `bun:"started_at"`
	CompletedAt  *time.Time `bun:"completed_at"`
	CreatedAt    time.Time  `bun:"created_at,notnull,default:now()"`
	UpdatedAt    time.Time  `bun:"updated_at,notnull,default:now()"`
}

// ------------------------------------------------------------------
// ObjectExtractionJob - Extracts entities/objects from content
// ------------------------------------------------------------------

// JobType represents the type of extraction job
type JobType string

const (
	JobTypeFullExtraction JobType = "full_extraction"
	JobTypeReextraction   JobType = "reextraction"
	JobTypeIncremental    JobType = "incremental"
)

// ObjectExtractionJob represents an object extraction job in kb.object_extraction_jobs.
// These jobs extract structured entities (people, organizations, concepts, etc.) from content.
type ObjectExtractionJob struct {
	bun.BaseModel `bun:"table:kb.object_extraction_jobs,alias:oej"`

	ID                   string     `bun:"id,pk,type:uuid,default:uuid_generate_v4()"`
	ProjectID            string     `bun:"project_id,notnull,type:uuid"`
	DocumentID           *string    `bun:"document_id,type:uuid"` // Optional: specific document
	ChunkID              *string    `bun:"chunk_id,type:uuid"`    // Optional: specific chunk
	JobType              JobType    `bun:"job_type,notnull,default:'full_extraction'"`
	Status               JobStatus  `bun:"status,notnull,default:'pending'"`
	EnabledTypes         []string   `bun:"enabled_types,array,notnull,default:'{}'"`          // Entity types to extract
	ExtractionConfig     JSON       `bun:"extraction_config,type:jsonb,notnull,default:'{}'"` // LLM/extraction settings
	ObjectsCreated       int        `bun:"objects_created,notnull,default:0"`
	RelationshipsCreated int        `bun:"relationships_created,notnull,default:0"`
	SuggestionsCreated   int        `bun:"suggestions_created,notnull,default:0"`
	TotalItems           int        `bun:"total_items,notnull,default:0"`
	ProcessedItems       int        `bun:"processed_items,notnull,default:0"`
	SuccessfulItems      int        `bun:"successful_items,notnull,default:0"`
	FailedItems          int        `bun:"failed_items,notnull,default:0"`
	StartedAt            *time.Time `bun:"started_at"`
	CompletedAt          *time.Time `bun:"completed_at"`
	ErrorMessage         *string    `bun:"error_message"`
	ErrorDetails         JSON       `bun:"error_details,type:jsonb"`
	RetryCount           int        `bun:"retry_count,notnull,default:0"`
	MaxRetries           int        `bun:"max_retries,notnull,default:3"`
	CreatedBy            *string    `bun:"created_by,type:uuid"`
	ReprocessingOf       *string    `bun:"reprocessing_of,type:uuid"` // ID of job being reprocessed
	SourceType           *string    `bun:"source_type"`               // 'document', 'chunk', 'manual'
	SourceID             *string    `bun:"source_id"`
	SourceMetadata       JSON       `bun:"source_metadata,type:jsonb,notnull,default:'{}'"`
	DebugInfo            JSON       `bun:"debug_info,type:jsonb"`
	Logs                 JSONArray  `bun:"logs,type:jsonb,notnull,default:'[]'"`
	DiscoveredTypes      JSONArray  `bun:"discovered_types,type:jsonb,default:'[]'"`
	CreatedObjects       JSONArray  `bun:"created_objects,type:jsonb,default:'[]'"`
	CreatedAt            time.Time  `bun:"created_at,notnull,default:now()"`
	UpdatedAt            time.Time  `bun:"updated_at,notnull,default:now()"`
}

// ------------------------------------------------------------------
// DataSourceSyncJob - Syncs data from external sources (ClickUp, etc.)
// ------------------------------------------------------------------

// TriggerType represents what triggered the sync
type TriggerType string

const (
	TriggerTypeManual    TriggerType = "manual"
	TriggerTypeScheduled TriggerType = "scheduled"
	TriggerTypeWebhook   TriggerType = "webhook"
)

// DataSourceSyncJob represents a data source sync job in kb.data_source_sync_jobs.
// These jobs sync data from external integrations (ClickUp, Notion, etc.).
type DataSourceSyncJob struct {
	bun.BaseModel `bun:"table:kb.data_source_sync_jobs,alias:dssj"`

	ID                string      `bun:"id,pk,type:uuid,default:gen_random_uuid()"`
	IntegrationID     string      `bun:"integration_id,notnull,type:uuid"`
	ProjectID         string      `bun:"project_id,notnull,type:uuid"`
	ConfigurationID   *string     `bun:"configuration_id,type:uuid"`
	ConfigurationName *string     `bun:"configuration_name"`
	Status            JobStatus   `bun:"status,notnull,default:'pending'"`
	TotalItems        int         `bun:"total_items,notnull,default:0"`
	ProcessedItems    int         `bun:"processed_items,notnull,default:0"`
	SuccessfulItems   int         `bun:"successful_items,notnull,default:0"`
	FailedItems       int         `bun:"failed_items,notnull,default:0"`
	SkippedItems      int         `bun:"skipped_items,notnull,default:0"`
	CurrentPhase      *string     `bun:"current_phase"` // 'fetching', 'processing', 'embedding', etc.
	StatusMessage     *string     `bun:"status_message"`
	SyncOptions       JSON        `bun:"sync_options,type:jsonb,notnull,default:'{}'"`
	DocumentIDs       JSONArray   `bun:"document_ids,type:jsonb,notnull,default:'[]'"` // Created document IDs
	Logs              JSONArray   `bun:"logs,type:jsonb,notnull,default:'[]'"`
	ErrorMessage      *string     `bun:"error_message"`
	ErrorDetails      JSON        `bun:"error_details,type:jsonb"`
	TriggeredBy       *string     `bun:"triggered_by,type:uuid"` // User who triggered
	TriggerType       TriggerType `bun:"trigger_type,notnull,default:'manual'"`
	CreatedAt         time.Time   `bun:"created_at,notnull,default:now()"`
	StartedAt         *time.Time  `bun:"started_at"`
	CompletedAt       *time.Time  `bun:"completed_at"`
	UpdatedAt         time.Time   `bun:"updated_at,notnull,default:now()"`
}

// ------------------------------------------------------------------
// Helper types
// ------------------------------------------------------------------

// JSON is a helper type for JSONB columns (objects)
type JSON map[string]interface{}

// Scan implements sql.Scanner for JSON
func (j *JSON) Scan(value interface{}) error {
	if value == nil {
		*j = nil
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return nil
	}
	return json.Unmarshal(bytes, j)
}

// JSONArray is a helper type for JSONB columns that store arrays
type JSONArray []interface{}

// Scan implements sql.Scanner for JSONArray
func (j *JSONArray) Scan(value interface{}) error {
	if value == nil {
		*j = nil
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return nil
	}
	return json.Unmarshal(bytes, j)
}

// LogEntry represents a log entry in job logs
type LogEntry struct {
	Timestamp time.Time `json:"timestamp"`
	Level     string    `json:"level"` // info, warn, error
	Message   string    `json:"message"`
	Details   JSON      `json:"details,omitempty"`
}

// ------------------------------------------------------------------
// ObjectExtractionLog - Detailed step-by-step logs for extraction jobs
// ------------------------------------------------------------------

// ExtractionLogOperationType represents the type of extraction operation
type ExtractionLogOperationType string

const (
	LogOpLLMCall              ExtractionLogOperationType = "llm_call"
	LogOpChunkProcessing      ExtractionLogOperationType = "chunk_processing"
	LogOpObjectCreation       ExtractionLogOperationType = "object_creation"
	LogOpRelationshipCreation ExtractionLogOperationType = "relationship_creation"
	LogOpSuggestionCreation   ExtractionLogOperationType = "suggestion_creation"
	LogOpValidation           ExtractionLogOperationType = "validation"
	LogOpError                ExtractionLogOperationType = "error"
)

// ExtractionLogStatus represents the status of a log step
type ExtractionLogStatus string

const (
	LogStatusQueued    ExtractionLogStatus = "queued"
	LogStatusRunning   ExtractionLogStatus = "running"
	LogStatusCompleted ExtractionLogStatus = "completed"
	LogStatusFailed    ExtractionLogStatus = "failed"
	LogStatusSkipped   ExtractionLogStatus = "skipped"
)

// ObjectExtractionLog represents a log entry in kb.object_extraction_logs.
// These logs provide step-by-step details of extraction operations.
type ObjectExtractionLog struct {
	bun.BaseModel `bun:"table:kb.object_extraction_logs,alias:oel"`

	ID                string     `bun:"id,pk,type:uuid,default:gen_random_uuid()"`
	ExtractionJobID   string     `bun:"extraction_job_id,notnull,type:uuid"`
	StartedAt         time.Time  `bun:"started_at,notnull,default:now()"`
	CompletedAt       *time.Time `bun:"completed_at"`
	StepIndex         int        `bun:"step_index,notnull"`
	OperationType     string     `bun:"operation_type,notnull"`
	OperationName     *string    `bun:"operation_name"`
	Step              string     `bun:"step,notnull"`
	Status            string     `bun:"status,notnull"`
	Message           *string    `bun:"message"`
	InputData         JSON       `bun:"input_data,type:jsonb"`
	OutputData        JSON       `bun:"output_data,type:jsonb"`
	ErrorMessage      *string    `bun:"error_message"`
	ErrorStack        *string    `bun:"error_stack"`
	ErrorDetails      JSON       `bun:"error_details,type:jsonb"`
	DurationMs        *int       `bun:"duration_ms"`
	TokensUsed        *int       `bun:"tokens_used"`
	EntityCount       *int       `bun:"entity_count"`
	RelationshipCount *int       `bun:"relationship_count"`
}

// ToDTO converts ObjectExtractionLog to ExtractionLogDTO
func (l *ObjectExtractionLog) ToDTO() *ExtractionLogDTO {
	dto := &ExtractionLogDTO{
		ID:                l.ID,
		ExtractionJobID:   l.ExtractionJobID,
		StartedAt:         l.StartedAt,
		CompletedAt:       l.CompletedAt,
		StepIndex:         l.StepIndex,
		OperationType:     l.OperationType,
		OperationName:     l.OperationName,
		Step:              l.Step,
		Status:            l.Status,
		Message:           l.Message,
		InputData:         l.InputData,
		OutputData:        l.OutputData,
		ErrorMessage:      l.ErrorMessage,
		ErrorStack:        l.ErrorStack,
		ErrorDetails:      l.ErrorDetails,
		DurationMs:        l.DurationMs,
		TokensUsed:        l.TokensUsed,
		EntityCount:       l.EntityCount,
		RelationshipCount: l.RelationshipCount,
	}
	return dto
}
