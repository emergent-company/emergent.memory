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
	RunStatusRunning   AgentRunStatus = "running"
	RunStatusSuccess   AgentRunStatus = "success"
	RunStatusSkipped   AgentRunStatus = "skipped"
	RunStatusError     AgentRunStatus = "error"
	RunStatusPaused    AgentRunStatus = "paused"
	RunStatusCancelled AgentRunStatus = "cancelled"

	// MaxTotalStepsPerRun is the global hard cap on cumulative steps across all resumes
	MaxTotalStepsPerRun = 500
)

// SessionStatus tracks the workspace provisioning lifecycle for an agent run.
// This is distinct from AgentRunStatus which tracks execution state.
type SessionStatus string

const (
	SessionStatusProvisioning SessionStatus = "provisioning" // Workspace being set up
	SessionStatusActive       SessionStatus = "active"       // Workspace ready, agent executing
	SessionStatusCompleted    SessionStatus = "completed"    // Run finished successfully
	SessionStatusError        SessionStatus = "error"        // Run or provisioning failed
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

	// Workspace session lifecycle tracking (distinct from run execution status)
	SessionStatus SessionStatus `bun:"session_status,notnull,default:'active'" json:"sessionStatus"`

	// Multi-agent coordination fields
	ParentRunID *string `bun:"parent_run_id,type:uuid" json:"parentRunId,omitempty"`
	StepCount   int     `bun:"step_count,notnull,default:0" json:"stepCount"`
	MaxSteps    *int    `bun:"max_steps" json:"maxSteps,omitempty"`
	ResumedFrom *string `bun:"resumed_from,type:uuid" json:"resumedFrom,omitempty"`

	// Relations
	Agent     *Agent    `bun:"rel:belongs-to,join:agent_id=id" json:"-"`
	ParentRun *AgentRun `bun:"rel:belongs-to,join:parent_run_id=id" json:"-"`
}

// CreateRunOptions holds options for creating an agent run with coordination support
type CreateRunOptions struct {
	AgentID          string
	ParentRunID      *string
	MaxSteps         *int
	ResumedFrom      *string
	InitialStepCount int // for resumed runs, start from prior run's step_count
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

// AgentVisibility defines the visibility level of an agent definition
type AgentVisibility string

const (
	VisibilityExternal AgentVisibility = "external" // Discoverable via ACP and admin UI
	VisibilityProject  AgentVisibility = "project"  // Visible in admin UI, not via ACP
	VisibilityInternal AgentVisibility = "internal" // Only visible to other agents
)

// AgentFlowType defines how an agent executes
type AgentFlowType string

const (
	FlowTypeSingle     AgentFlowType = "single"     // Single LLM agent
	FlowTypeSequential AgentFlowType = "sequential" // Sequential pipeline of steps
	FlowTypeLoop       AgentFlowType = "loop"       // Loop until condition met
)

// ACPConfig holds Agent Card Protocol metadata for externally-visible agents
type ACPConfig struct {
	DisplayName  string   `json:"displayName,omitempty"`
	Description  string   `json:"description,omitempty"`
	Capabilities []string `json:"capabilities,omitempty"`
	InputModes   []string `json:"inputModes,omitempty"`
	OutputModes  []string `json:"outputModes,omitempty"`
}

// ModelConfig holds model configuration for an agent definition
type ModelConfig struct {
	Name        string   `json:"name,omitempty"`
	Temperature *float32 `json:"temperature,omitempty"`
	MaxTokens   *int     `json:"maxTokens,omitempty"`
}

// AgentDefinition stores agent configurations from product manifests.
// This is separate from Agent (which tracks runtime state like last_run_at).
// Table: kb.agent_definitions
type AgentDefinition struct {
	bun.BaseModel `bun:"table:kb.agent_definitions,alias:ad"`

	ID              string          `bun:"id,pk,type:uuid,default:gen_random_uuid()" json:"id"`
	ProductID       *string         `bun:"product_id,type:uuid" json:"productId,omitempty"`
	ProjectID       string          `bun:"project_id,type:uuid,notnull" json:"projectId"`
	Name            string          `bun:"name,notnull" json:"name"`
	Description     *string         `bun:"description" json:"description,omitempty"`
	SystemPrompt    *string         `bun:"system_prompt" json:"systemPrompt,omitempty"`
	Model           *ModelConfig    `bun:"model,type:jsonb,default:'{}'" json:"model,omitempty"`
	Tools           []string        `bun:"tools,array" json:"tools"`
	FlowType        AgentFlowType   `bun:"flow_type,notnull,default:'single'" json:"flowType"`
	IsDefault       bool            `bun:"is_default,notnull,default:false" json:"isDefault"`
	MaxSteps        *int            `bun:"max_steps" json:"maxSteps,omitempty"`
	DefaultTimeout  *int            `bun:"default_timeout" json:"defaultTimeout,omitempty"`
	Visibility      AgentVisibility `bun:"visibility,notnull,default:'project'" json:"visibility"`
	ACPConfig       *ACPConfig      `bun:"acp_config,type:jsonb" json:"acpConfig,omitempty"`
	Config          map[string]any  `bun:"config,type:jsonb,default:'{}'" json:"config,omitempty"`
	WorkspaceConfig map[string]any  `bun:"workspace_config,type:jsonb" json:"workspaceConfig,omitempty"`
	CreatedAt       time.Time       `bun:"created_at,nullzero,notnull,default:current_timestamp" json:"createdAt"`
	UpdatedAt       time.Time       `bun:"updated_at,nullzero,notnull,default:current_timestamp" json:"updatedAt"`
}

// AgentRunMessage stores a single LLM message exchanged during an agent run.
// Messages are persisted in real-time during execution for crash recovery and resumption.
// Table: kb.agent_run_messages
type AgentRunMessage struct {
	bun.BaseModel `bun:"table:kb.agent_run_messages,alias:arm"`

	ID         string         `bun:"id,pk,type:uuid,default:gen_random_uuid()" json:"id"`
	RunID      string         `bun:"run_id,type:uuid,notnull" json:"runId"`
	Role       string         `bun:"role,notnull" json:"role"` // system, user, assistant, tool_result
	Content    map[string]any `bun:"content,type:jsonb,notnull,default:'{}'" json:"content"`
	StepNumber int            `bun:"step_number,notnull,default:0" json:"stepNumber"`
	CreatedAt  time.Time      `bun:"created_at,nullzero,notnull,default:current_timestamp" json:"createdAt"`

	// Relations
	Run *AgentRun `bun:"rel:belongs-to,join:run_id=id" json:"-"`
}

// AgentRunToolCall records a single tool invocation during an agent run.
// Table: kb.agent_run_tool_calls
type AgentRunToolCall struct {
	bun.BaseModel `bun:"table:kb.agent_run_tool_calls,alias:artc"`

	ID         string         `bun:"id,pk,type:uuid,default:gen_random_uuid()" json:"id"`
	RunID      string         `bun:"run_id,type:uuid,notnull" json:"runId"`
	MessageID  *string        `bun:"message_id,type:uuid" json:"messageId,omitempty"`
	ToolName   string         `bun:"tool_name,notnull" json:"toolName"`
	Input      map[string]any `bun:"input,type:jsonb,notnull,default:'{}'" json:"input"`
	Output     map[string]any `bun:"output,type:jsonb,notnull,default:'{}'" json:"output"`
	Status     string         `bun:"status,notnull,default:'completed'" json:"status"` // completed, error
	DurationMs *int           `bun:"duration_ms" json:"durationMs,omitempty"`
	StepNumber int            `bun:"step_number,notnull,default:0" json:"stepNumber"`
	CreatedAt  time.Time      `bun:"created_at,nullzero,notnull,default:current_timestamp" json:"createdAt"`

	// Relations
	Run     *AgentRun        `bun:"rel:belongs-to,join:run_id=id" json:"-"`
	Message *AgentRunMessage `bun:"rel:belongs-to,join:message_id=id" json:"-"`
}
