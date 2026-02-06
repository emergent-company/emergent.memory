package agents

import (
	"time"

	"github.com/uptrace/bun"
)

// AgentTriggerType defines how an agent is triggered
type AgentTriggerType string

const (
	TriggerTypeSchedule AgentTriggerType = "schedule"
	TriggerTypeManual   AgentTriggerType = "manual"
	TriggerTypeReaction AgentTriggerType = "reaction"
)

// AgentExecutionMode defines how the agent executes its actions
type AgentExecutionMode string

const (
	ExecutionModeSuggest AgentExecutionMode = "suggest"
	ExecutionModeExecute AgentExecutionMode = "execute"
	ExecutionModeHybrid  AgentExecutionMode = "hybrid"
)

// ReactionEventType defines events that can trigger a reaction agent
type ReactionEventType string

const (
	EventTypeCreated ReactionEventType = "created"
	EventTypeUpdated ReactionEventType = "updated"
	EventTypeDeleted ReactionEventType = "deleted"
)

// ConcurrencyStrategy defines how to handle concurrent events for the same object
type ConcurrencyStrategy string

const (
	ConcurrencySkip     ConcurrencyStrategy = "skip"
	ConcurrencyParallel ConcurrencyStrategy = "parallel"
)

// AgentRunStatus defines the status of an agent run
type AgentRunStatus string

const (
	RunStatusRunning AgentRunStatus = "running"
	RunStatusSuccess AgentRunStatus = "success"
	RunStatusSkipped AgentRunStatus = "skipped"
	RunStatusError   AgentRunStatus = "error"
)

// AgentProcessingStatus defines the status of agent processing for a graph object
type AgentProcessingStatus string

const (
	ProcessingStatusPending    AgentProcessingStatus = "pending"
	ProcessingStatusProcessing AgentProcessingStatus = "processing"
	ProcessingStatusCompleted  AgentProcessingStatus = "completed"
	ProcessingStatusFailed     AgentProcessingStatus = "failed"
	ProcessingStatusAbandoned  AgentProcessingStatus = "abandoned"
	ProcessingStatusSkipped    AgentProcessingStatus = "skipped"
)

// ReactionConfig contains configuration for reaction triggers
type ReactionConfig struct {
	ObjectTypes          []string            `json:"objectTypes"`
	Events               []ReactionEventType `json:"events"`
	ConcurrencyStrategy  ConcurrencyStrategy `json:"concurrencyStrategy"`
	IgnoreAgentTriggered bool                `json:"ignoreAgentTriggered"`
	IgnoreSelfTriggered  bool                `json:"ignoreSelfTriggered"`
}

// AgentCapabilities defines capability restrictions for agents
type AgentCapabilities struct {
	CanCreateObjects       *bool    `json:"canCreateObjects,omitempty"`
	CanUpdateObjects       *bool    `json:"canUpdateObjects,omitempty"`
	CanDeleteObjects       *bool    `json:"canDeleteObjects,omitempty"`
	CanCreateRelationships *bool    `json:"canCreateRelationships,omitempty"`
	AllowedObjectTypes     []string `json:"allowedObjectTypes,omitempty"`
}

// Agent represents a configurable background agent that runs periodically
// Table: kb.agents
type Agent struct {
	bun.BaseModel `bun:"table:kb.agents,alias:a"`

	ID             string             `bun:"id,pk,type:uuid,default:gen_random_uuid()" json:"id"`
	ProjectID      string             `bun:"project_id,type:uuid,notnull" json:"projectId"`
	Name           string             `bun:"name,notnull" json:"name"`
	StrategyType   string             `bun:"strategy_type,notnull" json:"strategyType"`
	Prompt         *string            `bun:"prompt" json:"prompt"`
	CronSchedule   string             `bun:"cron_schedule,notnull" json:"cronSchedule"`
	Enabled        bool               `bun:"enabled,notnull,default:true" json:"enabled"`
	TriggerType    AgentTriggerType   `bun:"trigger_type,notnull,default:'schedule'" json:"triggerType"`
	ReactionConfig *ReactionConfig    `bun:"reaction_config,type:jsonb" json:"reactionConfig"`
	ExecutionMode  AgentExecutionMode `bun:"execution_mode,notnull,default:'execute'" json:"executionMode"`
	Capabilities   *AgentCapabilities `bun:"capabilities,type:jsonb" json:"capabilities"`
	Config         map[string]any     `bun:"config,type:jsonb,default:'{}'" json:"config"`
	Description    *string            `bun:"description" json:"description"`
	LastRunAt      *time.Time         `bun:"last_run_at" json:"lastRunAt"`
	LastRunStatus  *string            `bun:"last_run_status" json:"lastRunStatus"`
	CreatedAt      time.Time          `bun:"created_at,nullzero,notnull,default:current_timestamp" json:"createdAt"`
	UpdatedAt      time.Time          `bun:"updated_at,nullzero,notnull,default:current_timestamp" json:"updatedAt"`
}

// AgentRun records each execution of an agent for observability
// Table: kb.agent_runs
type AgentRun struct {
	bun.BaseModel `bun:"table:kb.agent_runs,alias:ar"`

	ID           string         `bun:"id,pk,type:uuid,default:gen_random_uuid()" json:"id"`
	AgentID      string         `bun:"agent_id,type:uuid,notnull" json:"agentId"`
	Status       AgentRunStatus `bun:"status,notnull" json:"status"`
	StartedAt    time.Time      `bun:"started_at,notnull" json:"startedAt"`
	CompletedAt  *time.Time     `bun:"completed_at" json:"completedAt"`
	DurationMs   *int           `bun:"duration_ms" json:"durationMs"`
	Summary      map[string]any `bun:"summary,type:jsonb,default:'{}'" json:"summary"`
	ErrorMessage *string        `bun:"error_message" json:"errorMessage"`
	SkipReason   *string        `bun:"skip_reason" json:"skipReason"`
	CreatedAt    time.Time      `bun:"created_at,nullzero,notnull,default:current_timestamp" json:"createdAt"`

	// Relations
	Agent *Agent `bun:"rel:belongs-to,join:agent_id=id" json:"-"`
}

// AgentProcessingLog tracks which graph objects have been processed by reaction agents
// Table: kb.agent_processing_log
type AgentProcessingLog struct {
	bun.BaseModel `bun:"table:kb.agent_processing_log,alias:apl"`

	ID            string                `bun:"id,pk,type:uuid,default:gen_random_uuid()" json:"id"`
	AgentID       string                `bun:"agent_id,type:uuid,notnull" json:"agentId"`
	GraphObjectID string                `bun:"graph_object_id,type:uuid,notnull" json:"graphObjectId"`
	ObjectVersion int                   `bun:"object_version,notnull" json:"objectVersion"`
	EventType     ReactionEventType     `bun:"event_type,notnull" json:"eventType"`
	Status        AgentProcessingStatus `bun:"status,notnull,default:'pending'" json:"status"`
	StartedAt     *time.Time            `bun:"started_at" json:"startedAt"`
	CompletedAt   *time.Time            `bun:"completed_at" json:"completedAt"`
	ErrorMessage  *string               `bun:"error_message" json:"errorMessage"`
	ResultSummary map[string]any        `bun:"result_summary,type:jsonb" json:"resultSummary"`
	CreatedAt     time.Time             `bun:"created_at,nullzero,notnull,default:current_timestamp" json:"createdAt"`

	// Relations
	Agent *Agent `bun:"rel:belongs-to,join:agent_id=id" json:"-"`
}
