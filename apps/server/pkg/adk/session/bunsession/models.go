package bunsession

import (
	"time"

	"github.com/uptrace/bun"
)

// ADKSession corresponds to the 'kb.adk_sessions' table.
type ADKSession struct {
	bun.BaseModel `bun:"table:kb.adk_sessions,alias:as"`

	ID         string                 `bun:"id,pk,type:text"` // Note: ADK uses client-provided strings, not necessarily UUID
	AppName    string                 `bun:"app_name,pk,type:text"`
	UserID     string                 `bun:"user_id,pk,type:text"`
	State      map[string]interface{} `bun:"state,type:jsonb,nullzero"`
	CreateTime time.Time              `bun:"create_time,notnull,default:current_timestamp"`
	UpdateTime time.Time              `bun:"update_time,notnull,default:current_timestamp"`
}

// ADKEvent corresponds to the 'kb.adk_events' table.
type ADKEvent struct {
	bun.BaseModel `bun:"table:kb.adk_events,alias:ae"`

	ID        string `bun:"id,pk,type:text"`
	AppName   string `bun:"app_name,notnull,type:text"`
	UserID    string `bun:"user_id,notnull,type:text"`
	SessionID string `bun:"session_id,notnull,type:text"`

	InvocationID           string    `bun:"invocation_id,type:text"`
	Author                 string    `bun:"author,type:text"`
	Actions                []byte    `bun:"actions,type:jsonb"`
	LongRunningToolIDsJSON []byte    `bun:"long_running_tool_ids_json,type:jsonb"`
	Branch                 *string   `bun:"branch,type:text"`
	Timestamp              time.Time `bun:"timestamp,notnull,default:current_timestamp"`

	Content           []byte `bun:"content,type:jsonb"`
	GroundingMetadata []byte `bun:"grounding_metadata,type:jsonb"`
	CustomMetadata    []byte `bun:"custom_metadata,type:jsonb"`
	UsageMetadata     []byte `bun:"usage_metadata,type:jsonb"`
	CitationMetadata  []byte `bun:"citation_metadata,type:jsonb"`

	Partial      *bool   `bun:"partial"`
	TurnComplete *bool   `bun:"turn_complete"`
	ErrorCode    *string `bun:"error_code,type:text"`
	ErrorMessage *string `bun:"error_message,type:text"`
	Interrupted  *bool   `bun:"interrupted"`
}

// ADKState corresponds to the 'kb.adk_states' table for app and user level state.
type ADKState struct {
	bun.BaseModel `bun:"table:kb.adk_states,alias:ast"`

	Scope      string                 `bun:"scope,pk,type:text"` // 'app', 'user'
	AppName    string                 `bun:"app_name,pk,type:text"`
	UserID     string                 `bun:"user_id,pk,type:text"` // empty for 'app' scope
	SessionID  string                 `bun:"session_id,type:text"` // For task 1.3 FK requirement, though ADK uses session state in the session table usually.
	State      map[string]interface{} `bun:"state,type:jsonb,nullzero"`
	UpdateTime time.Time              `bun:"update_time,notnull,default:current_timestamp"`
}
