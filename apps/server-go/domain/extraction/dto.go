package extraction

import "time"

// ExtractionJobStatus represents the status of an extraction job (used in DTOs)
// Maps to internal JobStatus but with NestJS-compatible values
type ExtractionJobStatusDTO string

const (
	StatusQueued         ExtractionJobStatusDTO = "queued"
	StatusRunning        ExtractionJobStatusDTO = "running"
	StatusCompleted      ExtractionJobStatusDTO = "completed"
	StatusRequiresReview ExtractionJobStatusDTO = "requires_review"
	StatusFailed         ExtractionJobStatusDTO = "failed"
	StatusCancelled      ExtractionJobStatusDTO = "cancelled"
)

// ExtractionSourceType represents the source type of an extraction job
type ExtractionSourceType string

const (
	SourceTypeDocument   ExtractionSourceType = "document"
	SourceTypeAPI        ExtractionSourceType = "api"
	SourceTypeManual     ExtractionSourceType = "manual"
	SourceTypeBulkImport ExtractionSourceType = "bulk_import"
)

// ExtractionJobDTO is the response DTO for an extraction job
type ExtractionJobDTO struct {
	ID               string                 `json:"id"`
	ProjectID        string                 `json:"project_id"`
	SourceType       ExtractionSourceType   `json:"source_type"`
	SourceID         *string                `json:"source_id,omitempty"`
	SourceMetadata   map[string]interface{} `json:"source_metadata"`
	ExtractionConfig map[string]interface{} `json:"extraction_config"`
	Status           ExtractionJobStatusDTO `json:"status"`
	TotalItems       int                    `json:"total_items"`
	ProcessedItems   int                    `json:"processed_items"`
	SuccessfulItems  int                    `json:"successful_items"`
	FailedItems      int                    `json:"failed_items"`
	DiscoveredTypes  []string               `json:"discovered_types"`
	CreatedObjects   []string               `json:"created_objects"`
	ErrorMessage     *string                `json:"error_message,omitempty"`
	ErrorDetails     map[string]interface{} `json:"error_details,omitempty"`
	DebugInfo        map[string]interface{} `json:"debug_info,omitempty"`
	StartedAt        *time.Time             `json:"started_at,omitempty"`
	CompletedAt      *time.Time             `json:"completed_at,omitempty"`
	CreatedAt        time.Time              `json:"created_at"`
	SubjectID        *string                `json:"subject_id,omitempty"`
	UpdatedAt        time.Time              `json:"updated_at"`
}

// CreateExtractionJobDTO is the request DTO for creating an extraction job
type CreateExtractionJobDTO struct {
	ProjectID        *string                `json:"project_id"`
	SourceType       ExtractionSourceType   `json:"source_type" validate:"required"`
	SourceID         *string                `json:"source_id"`
	SourceMetadata   map[string]interface{} `json:"source_metadata"`
	ExtractionConfig map[string]interface{} `json:"extraction_config" validate:"required"`
	SubjectID        *string                `json:"subject_id"`
}

// UpdateExtractionJobDTO is the request DTO for updating an extraction job
type UpdateExtractionJobDTO struct {
	Status          *ExtractionJobStatusDTO `json:"status"`
	TotalItems      *int                    `json:"total_items"`
	ProcessedItems  *int                    `json:"processed_items"`
	SuccessfulItems *int                    `json:"successful_items"`
	FailedItems     *int                    `json:"failed_items"`
	DiscoveredTypes []string                `json:"discovered_types"`
	CreatedObjects  []string                `json:"created_objects"`
	ErrorMessage    *string                 `json:"error_message"`
	ErrorDetails    map[string]interface{}  `json:"error_details"`
	DebugInfo       map[string]interface{}  `json:"debug_info"`
}

// ListExtractionJobsParams contains query parameters for listing jobs
type ListExtractionJobsParams struct {
	Status     *ExtractionJobStatusDTO `query:"status"`
	SourceType *ExtractionSourceType   `query:"source_type"`
	SourceID   *string                 `query:"source_id"`
	Page       int                     `query:"page"`
	Limit      int                     `query:"limit"`
}

// ExtractionJobListDTO is the paginated response for listing jobs
type ExtractionJobListDTO struct {
	Jobs       []*ExtractionJobDTO `json:"jobs"`
	Total      int                 `json:"total"`
	Page       int                 `json:"page"`
	Limit      int                 `json:"limit"`
	TotalPages int                 `json:"total_pages"`
}

// ExtractionJobStatisticsDTO contains aggregated statistics
type ExtractionJobStatisticsDTO struct {
	TotalJobs               int                            `json:"total_jobs"`
	JobsByStatus            map[ExtractionJobStatusDTO]int `json:"jobs_by_status"`
	SuccessRate             float64                        `json:"success_rate"`
	AverageProcessingTimeMs *int64                         `json:"average_processing_time_ms,omitempty"`
	MostExtractedTypes      []TypeCountDTO                 `json:"most_extracted_types"`
	JobsThisWeek            int                            `json:"jobs_this_week"`
	JobsThisMonth           int                            `json:"jobs_this_month"`
}

// TypeCountDTO represents a type and its count
type TypeCountDTO struct {
	Type  string `json:"type"`
	Count int    `json:"count"`
}

// BulkOperationResponseDTO is the response for bulk operations
type BulkOperationResponseDTO struct {
	Count   int    `json:"count"`
	Message string `json:"message"`
}

// BulkCancelResponseDTO is the response for bulk cancel
type BulkCancelResponseDTO struct {
	Cancelled int    `json:"cancelled"`
	Message   string `json:"message"`
}

// BulkDeleteResponseDTO is the response for bulk delete
type BulkDeleteResponseDTO struct {
	Deleted int    `json:"deleted"`
	Message string `json:"message"`
}

// BulkRetryResponseDTO is the response for bulk retry
type BulkRetryResponseDTO struct {
	Retried int    `json:"retried"`
	Message string `json:"message"`
}

// ------------------------------------------------------------------
// Extraction Log DTOs
// ------------------------------------------------------------------

// ExtractionLogDTO is the response DTO for an extraction log entry
type ExtractionLogDTO struct {
	ID                string                 `json:"id"`
	ExtractionJobID   string                 `json:"extraction_job_id"`
	StartedAt         time.Time              `json:"started_at"`
	CompletedAt       *time.Time             `json:"completed_at,omitempty"`
	StepIndex         int                    `json:"step_index"`
	OperationType     string                 `json:"operation_type"`
	OperationName     *string                `json:"operation_name,omitempty"`
	Step              string                 `json:"step"`
	Status            string                 `json:"status"`
	Message           *string                `json:"message,omitempty"`
	InputData         map[string]interface{} `json:"input_data,omitempty"`
	OutputData        map[string]interface{} `json:"output_data,omitempty"`
	ErrorMessage      *string                `json:"error_message,omitempty"`
	ErrorStack        *string                `json:"error_stack,omitempty"`
	ErrorDetails      map[string]interface{} `json:"error_details,omitempty"`
	DurationMs        *int                   `json:"duration_ms,omitempty"`
	TokensUsed        *int                   `json:"tokens_used,omitempty"`
	EntityCount       *int                   `json:"entity_count,omitempty"`
	RelationshipCount *int                   `json:"relationship_count,omitempty"`
}

// ExtractionLogSummaryDTO contains summary statistics for extraction logs
type ExtractionLogSummaryDTO struct {
	TotalSteps      int            `json:"totalSteps"`
	SuccessSteps    int            `json:"successSteps"`
	ErrorSteps      int            `json:"errorSteps"`
	WarningSteps    int            `json:"warningSteps"`
	TotalDurationMs int            `json:"totalDurationMs"`
	TotalTokensUsed int            `json:"totalTokensUsed"`
	OperationCounts map[string]int `json:"operationCounts"`
}

// TimelineEventDTO represents an event in the job timeline (from debug_info)
type TimelineEventDTO map[string]interface{}

// ExtractionLogsResponseDTO is the response for the logs endpoint
type ExtractionLogsResponseDTO struct {
	Logs     []*ExtractionLogDTO     `json:"logs"`
	Summary  ExtractionLogSummaryDTO `json:"summary"`
	Timeline []TimelineEventDTO      `json:"timeline,omitempty"`
}

// APIResponse wraps API responses with success flag
type APIResponse[T any] struct {
	Success bool    `json:"success"`
	Data    T       `json:"data,omitempty"`
	Error   *string `json:"error,omitempty"`
	Message *string `json:"message,omitempty"`
}

// SuccessResponse creates a successful API response
func SuccessResponse[T any](data T) APIResponse[T] {
	return APIResponse[T]{
		Success: true,
		Data:    data,
	}
}

// ErrorResponse creates an error API response
func ErrorResponse[T any](err string) APIResponse[T] {
	return APIResponse[T]{
		Success: false,
		Error:   &err,
	}
}

// mapJobStatusToDTO converts internal JobStatus to DTO status
func mapJobStatusToDTO(status JobStatus) ExtractionJobStatusDTO {
	switch status {
	case JobStatusPending:
		return StatusQueued
	case JobStatusProcessing:
		return StatusRunning
	case JobStatusCompleted:
		return StatusCompleted
	case JobStatusFailed, JobStatusDeadLetter:
		return StatusFailed
	case JobStatusCancelled:
		return StatusCancelled
	default:
		return StatusQueued
	}
}

// mapJobStatusFromDTO converts DTO status to internal JobStatus
func mapJobStatusFromDTO(status ExtractionJobStatusDTO) JobStatus {
	switch status {
	case StatusQueued:
		return JobStatusPending
	case StatusRunning:
		return JobStatusProcessing
	case StatusCompleted:
		return JobStatusCompleted
	case StatusRequiresReview:
		return JobStatusCompleted // requires_review is a subset of completed
	case StatusFailed:
		return JobStatusFailed
	case StatusCancelled:
		return JobStatusCancelled
	default:
		return JobStatusPending
	}
}

// ToDTO converts an ObjectExtractionJob entity to ExtractionJobDTO
func (j *ObjectExtractionJob) ToDTO() *ExtractionJobDTO {
	// Convert source metadata
	sourceMetadata := make(map[string]interface{})
	if j.SourceMetadata != nil {
		sourceMetadata = j.SourceMetadata
	}

	// Convert extraction config
	extractionConfig := make(map[string]interface{})
	if j.ExtractionConfig != nil {
		extractionConfig = j.ExtractionConfig
	}

	// Convert discovered types from JSONArray to []string
	discoveredTypes := make([]string, 0)
	if j.DiscoveredTypes != nil {
		for _, v := range j.DiscoveredTypes {
			if s, ok := v.(string); ok {
				discoveredTypes = append(discoveredTypes, s)
			}
		}
	}

	// Convert created objects from JSONArray to []string
	createdObjects := make([]string, 0)
	if j.CreatedObjects != nil {
		for _, v := range j.CreatedObjects {
			if s, ok := v.(string); ok {
				createdObjects = append(createdObjects, s)
			}
		}
	}

	// Convert error details
	var errorDetails map[string]interface{}
	if j.ErrorDetails != nil {
		errorDetails = j.ErrorDetails
	}

	// Convert debug info
	var debugInfo map[string]interface{}
	if j.DebugInfo != nil {
		debugInfo = j.DebugInfo
	}

	// Determine source type (default to document if not set)
	sourceType := SourceTypeDocument
	if j.SourceType != nil {
		switch *j.SourceType {
		case "document":
			sourceType = SourceTypeDocument
		case "api":
			sourceType = SourceTypeAPI
		case "manual":
			sourceType = SourceTypeManual
		case "bulk_import":
			sourceType = SourceTypeBulkImport
		default:
			sourceType = ExtractionSourceType(*j.SourceType)
		}
	}

	return &ExtractionJobDTO{
		ID:               j.ID,
		ProjectID:        j.ProjectID,
		SourceType:       sourceType,
		SourceID:         j.SourceID,
		SourceMetadata:   sourceMetadata,
		ExtractionConfig: extractionConfig,
		Status:           mapJobStatusToDTO(j.Status),
		TotalItems:       j.TotalItems,
		ProcessedItems:   j.ProcessedItems,
		SuccessfulItems:  j.SuccessfulItems,
		FailedItems:      j.FailedItems,
		DiscoveredTypes:  discoveredTypes,
		CreatedObjects:   createdObjects,
		ErrorMessage:     j.ErrorMessage,
		ErrorDetails:     errorDetails,
		DebugInfo:        debugInfo,
		StartedAt:        j.StartedAt,
		CompletedAt:      j.CompletedAt,
		CreatedAt:        j.CreatedAt,
		SubjectID:        j.CreatedBy,
		UpdatedAt:        j.UpdatedAt,
	}
}
