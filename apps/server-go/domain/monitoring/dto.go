package monitoring

import (
	"time"
)

// ExtractionJobSummaryDTO represents a summary of an extraction job for list views
type ExtractionJobSummaryDTO struct {
	ID                   string     `json:"id"`
	SourceType           string     `json:"source_type"`
	SourceID             string     `json:"source_id"`
	Status               string     `json:"status"`
	StartedAt            *time.Time `json:"started_at,omitempty"`
	CompletedAt          *time.Time `json:"completed_at,omitempty"`
	DurationMs           *int       `json:"duration_ms,omitempty"`
	ObjectsCreated       *int       `json:"objects_created,omitempty"`
	RelationshipsCreated *int       `json:"relationships_created,omitempty"`
	SuggestionsCreated   *int       `json:"suggestions_created,omitempty"`
	TotalLLMCalls        *int       `json:"total_llm_calls,omitempty"`
	TotalCostUSD         *float64   `json:"total_cost_usd,omitempty"`
	ErrorMessage         *string    `json:"error_message,omitempty"`
}

// ProcessLogDTO represents a system process log entry
type ProcessLogDTO struct {
	ID          string                 `json:"id"`
	Timestamp   time.Time              `json:"timestamp"`
	Level       string                 `json:"level"`
	Message     string                 `json:"message"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
	ProcessType string                 `json:"processType,omitempty"`
}

// LLMCallLogDTO represents an LLM API call log entry
type LLMCallLogDTO struct {
	ID              string                 `json:"id"`
	ModelName       string                 `json:"model_name"`
	Status          string                 `json:"status"`
	InputTokens     *int                   `json:"input_tokens,omitempty"`
	OutputTokens    *int                   `json:"output_tokens,omitempty"`
	TotalTokens     *int                   `json:"total_tokens,omitempty"`
	CostUSD         *float64               `json:"cost_usd,omitempty"`
	DurationMs      *int                   `json:"duration_ms,omitempty"`
	StartedAt       time.Time              `json:"started_at"`
	CompletedAt     *time.Time             `json:"completed_at,omitempty"`
	RequestPayload  map[string]interface{} `json:"request_payload,omitempty"`
	ResponsePayload map[string]interface{} `json:"response_payload,omitempty"`
	ErrorMessage    *string                `json:"error_message,omitempty"`
}

// ExtractionJobMetricsDTO represents aggregated metrics for an extraction job
type ExtractionJobMetricsDTO struct {
	TotalLLMCalls     int     `json:"total_llm_calls"`
	TotalCostUSD      float64 `json:"total_cost_usd"`
	TotalTokens       int     `json:"total_tokens"`
	AvgCallDurationMs float64 `json:"avg_call_duration_ms"`
	SuccessRate       float64 `json:"success_rate"`
}

// ExtractionJobDetailDTO represents full details for an extraction job
type ExtractionJobDetailDTO struct {
	ID                   string                   `json:"id"`
	SourceType           string                   `json:"source_type"`
	SourceID             string                   `json:"source_id"`
	Status               string                   `json:"status"`
	StartedAt            *time.Time               `json:"started_at,omitempty"`
	CompletedAt          *time.Time               `json:"completed_at,omitempty"`
	DurationMs           *int                     `json:"duration_ms,omitempty"`
	ObjectsCreated       *int                     `json:"objects_created,omitempty"`
	RelationshipsCreated *int                     `json:"relationships_created,omitempty"`
	SuggestionsCreated   *int                     `json:"suggestions_created,omitempty"`
	ErrorMessage         *string                  `json:"error_message,omitempty"`
	Logs                 []ProcessLogDTO          `json:"logs"`
	LLMCalls             []LLMCallLogDTO          `json:"llm_calls"`
	Metrics              *ExtractionJobMetricsDTO `json:"metrics,omitempty"`
}

// ExtractionJobListResponseDTO represents a paginated list of extraction jobs
type ExtractionJobListResponseDTO struct {
	Items  []ExtractionJobSummaryDTO `json:"items"`
	Total  int                       `json:"total"`
	Limit  int                       `json:"limit"`
	Offset int                       `json:"offset"`
}

// ListExtractionJobsParams represents query parameters for listing extraction jobs
type ListExtractionJobsParams struct {
	Type       string `query:"type"`
	Status     string `query:"status"`
	SourceType string `query:"source_type"`
	DateFrom   string `query:"date_from"`
	DateTo     string `query:"date_to"`
	Limit      int    `query:"limit"`
	Offset     int    `query:"offset"`
	SortBy     string `query:"sort_by"`
	SortOrder  string `query:"sort_order"`
}

// LogQueryParams represents query parameters for fetching logs
type LogQueryParams struct {
	Level  string `query:"level"`
	Limit  int    `query:"limit"`
	Offset int    `query:"offset"`
}

// ProcessLogListDTO wraps logs for response
type ProcessLogListDTO struct {
	Logs []ProcessLogDTO `json:"logs"`
}

// LLMCallListDTO wraps LLM calls for response
type LLMCallListDTO struct {
	LLMCalls []LLMCallLogDTO `json:"llm_calls"`
}

// ToProcessLogDTO converts SystemProcessLog entity to DTO
func (l *SystemProcessLog) ToDTO() ProcessLogDTO {
	return ProcessLogDTO{
		ID:          l.ID,
		Timestamp:   l.Timestamp,
		Level:       l.Level,
		Message:     l.Message,
		Metadata:    l.Metadata,
		ProcessType: l.ProcessType,
	}
}

// ToLLMCallLogDTO converts LLMCallLog entity to DTO
func (l *LLMCallLog) ToDTO() LLMCallLogDTO {
	return LLMCallLogDTO{
		ID:              l.ID,
		ModelName:       l.ModelName,
		Status:          l.Status,
		InputTokens:     l.InputTokens,
		OutputTokens:    l.OutputTokens,
		TotalTokens:     l.TotalTokens,
		CostUSD:         l.CostUSD,
		DurationMs:      l.DurationMs,
		StartedAt:       l.StartedAt,
		CompletedAt:     l.CompletedAt,
		RequestPayload:  l.RequestPayload,
		ResponsePayload: l.ResponsePayload,
		ErrorMessage:    l.ErrorMessage,
	}
}
