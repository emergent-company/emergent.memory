package monitoring

import (
	"time"

	"github.com/uptrace/bun"
)

// SystemProcessLog represents a log entry from kb.system_process_logs
type SystemProcessLog struct {
	bun.BaseModel `bun:"table:kb.system_process_logs,alias:spl"`

	ID              string                 `bun:"id,pk,type:uuid,default:uuid_generate_v4()" json:"id"`
	ProcessID       string                 `bun:"process_id,notnull" json:"process_id"`
	ProcessType     string                 `bun:"process_type,notnull" json:"process_type"`
	Level           string                 `bun:"level,notnull" json:"level"`
	Message         string                 `bun:"message,notnull" json:"message"`
	Metadata        map[string]interface{} `bun:"metadata,type:jsonb" json:"metadata,omitempty"`
	Timestamp       time.Time              `bun:"timestamp,notnull,default:now()" json:"timestamp"`
	LangfuseTraceID *string                `bun:"langfuse_trace_id" json:"langfuse_trace_id,omitempty"`
}

// LLMCallLog represents a log entry from kb.llm_call_logs
type LLMCallLog struct {
	bun.BaseModel `bun:"table:kb.llm_call_logs,alias:lcl"`

	ID                    string                 `bun:"id,pk,type:uuid,default:uuid_generate_v4()" json:"id"`
	ProcessID             string                 `bun:"process_id,notnull" json:"process_id"`
	ProcessType           string                 `bun:"process_type,notnull" json:"process_type"`
	ModelName             string                 `bun:"model_name,notnull" json:"model_name"`
	RequestPayload        map[string]interface{} `bun:"request_payload,type:jsonb" json:"request_payload,omitempty"`
	ResponsePayload       map[string]interface{} `bun:"response_payload,type:jsonb" json:"response_payload,omitempty"`
	Status                string                 `bun:"status,notnull" json:"status"`
	ErrorMessage          *string                `bun:"error_message" json:"error_message,omitempty"`
	InputTokens           *int                   `bun:"input_tokens" json:"input_tokens,omitempty"`
	OutputTokens          *int                   `bun:"output_tokens" json:"output_tokens,omitempty"`
	TotalTokens           *int                   `bun:"total_tokens" json:"total_tokens,omitempty"`
	CostUSD               *float64               `bun:"cost_usd,type:numeric(10,6)" json:"cost_usd,omitempty"`
	StartedAt             time.Time              `bun:"started_at,notnull,default:now()" json:"started_at"`
	CompletedAt           *time.Time             `bun:"completed_at" json:"completed_at,omitempty"`
	DurationMs            *int                   `bun:"duration_ms" json:"duration_ms,omitempty"`
	LangfuseObservationID *string                `bun:"langfuse_observation_id" json:"langfuse_observation_id,omitempty"`
}
