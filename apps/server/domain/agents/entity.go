package agents

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/domain/agents/toolgroups"
	"github.com/emergent-company/emergent.memory/domain/mcp"
	"github.com/emergent-company/emergent.memory/pkg/runstatus"
)

// AgentTriggerType defines how an agent is triggered
type AgentTriggerType string

const (
	TriggerTypeSchedule AgentTriggerType = "schedule"
	TriggerTypeManual   AgentTriggerType = "manual"
	TriggerTypeReaction AgentTriggerType = "reaction"
	TriggerTypeWebhook  AgentTriggerType = "webhook"
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

// AgentDispatchMode controls how trigger_agent schedules execution of an agent.
// This is distinct from AgentExecutionMode (which is the UI interaction mode).
type AgentDispatchMode string

const (
	// DispatchModeSync is the default: trigger_agent blocks until the run completes.
	DispatchModeSync AgentDispatchMode = "sync"
	// DispatchModeQueued: trigger_agent enqueues the run and returns run_id immediately.
	// A worker pool picks up and executes the run asynchronously.
	DispatchModeQueued AgentDispatchMode = "queued"
)

// AgentRunStatus defines the status of an agent run. The canonical string values
// live in pkg/runstatus so domain/scheduler can filter terminal runs without
// importing this package (an import cycle: domain/agents imports domain/scheduler).
type AgentRunStatus string

const (
	RunStatusQueued     AgentRunStatus = AgentRunStatus(runstatus.Queued)
	RunStatusRunning    AgentRunStatus = AgentRunStatus(runstatus.Running)
	RunStatusSuccess    AgentRunStatus = AgentRunStatus(runstatus.Success)
	RunStatusSkipped    AgentRunStatus = AgentRunStatus(runstatus.Skipped)
	RunStatusError      AgentRunStatus = AgentRunStatus(runstatus.Error)
	RunStatusPaused     AgentRunStatus = AgentRunStatus(runstatus.Paused)
	RunStatusCancelled  AgentRunStatus = AgentRunStatus(runstatus.Cancelled)
	RunStatusCancelling AgentRunStatus = AgentRunStatus(runstatus.Cancelling)

	// MaxTotalStepsPerRun is the global hard cap on cumulative steps across all resumes
	MaxTotalStepsPerRun = 500
	// DefaultMaxStepsPerRun is the default step limit when no max_steps is configured
	// on the agent definition or trigger request.
	DefaultMaxStepsPerRun = 30
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
	ObjectTypes []string            `json:"objectTypes"`
	Events      []ReactionEventType `json:"events"`
	// ConcurrencyStrategy controls how a new trigger is handled while a
	// non-terminal run already exists for the same agent + target object.
	// Empty ("") and "parallel" both mean no concurrency control; "skip" drops
	// the new trigger instead of starting a second run.
	ConcurrencyStrategy ConcurrencyStrategy `json:"concurrencyStrategy"`
	// IgnoreAgentTriggered controls whether agent-originated events are ignored.
	// nil (unset) = true = ignore, preserving the historical loop-safety default.
	// Explicit false opts this agent in to agent-originated events.
	IgnoreAgentTriggered *bool `json:"ignoreAgentTriggered,omitempty"`
	// IgnoreSelfTriggered controls whether this agent's own runs may re-trigger
	// it. nil (unset) = true = ignore. Explicit false allows self-triggering.
	// Only consulted when IgnoreAgentTriggered is explicitly false.
	IgnoreSelfTriggered *bool `json:"ignoreSelfTriggered,omitempty"`
}

// AgentCapabilities defines capability restrictions for agents
type AgentCapabilities struct {
	CanCreateObjects       *bool    `json:"canCreateObjects,omitempty"`
	CanUpdateObjects       *bool    `json:"canUpdateObjects,omitempty"`
	CanDeleteObjects       *bool    `json:"canDeleteObjects,omitempty"`
	CanCreateRelationships *bool    `json:"canCreateRelationships,omitempty"`
	AllowedObjectTypes     []string `json:"allowedObjectTypes,omitempty"`
}

// RateLimitConfig configures rate limiting for an agent webhook hook
type RateLimitConfig struct {
	RequestsPerMinute int `json:"requestsPerMinute"`
	BurstSize         int `json:"burstSize"`
}

// AgentWebhookHook represents a configurable webhook endpoint for triggering an agent
// Table: kb.agent_webhook_hooks
type AgentWebhookHook struct {
	bun.BaseModel `bun:"table:kb.agent_webhook_hooks,alias:awh"`

	ID              string           `bun:"id,pk,type:uuid,default:gen_random_uuid()" json:"id"`
	AgentID         string           `bun:"agent_id,type:uuid,notnull" json:"agentId"`
	ProjectID       string           `bun:"project_id,type:text,notnull" json:"projectId"`
	Label           string           `bun:"label,notnull" json:"label"`
	TokenHash       string           `bun:"token_hash,notnull" json:"-"` // Never expose hash in JSON
	Enabled         bool             `bun:"enabled,notnull,default:true" json:"enabled"`
	AllowInternal   bool             `bun:"allow_internal,notnull,default:false" json:"allowInternal"` // opt-in to bind an internal-visibility agent (see #1004)
	RateLimitConfig *RateLimitConfig `bun:"rate_limit_config,type:jsonb" json:"rateLimitConfig"`
	CreatedAt       time.Time        `bun:"created_at,nullzero,notnull,default:current_timestamp" json:"createdAt"`
	UpdatedAt       time.Time        `bun:"updated_at,nullzero,notnull,default:current_timestamp" json:"updatedAt"`

	// Transient field for returning a newly generated token (never persisted)
	Token *string `bun:"-" json:"token,omitempty"`
}

// Agent represents a configurable background agent that runs periodically
// Table: kb.agents
type Agent struct {
	bun.BaseModel `bun:"table:kb.agents,alias:a"`

	ID                  string             `bun:"id,pk,type:uuid,default:gen_random_uuid()" json:"id"`
	ProjectID           string             `bun:"project_id,type:uuid,notnull" json:"projectId"`
	Name                string             `bun:"name,notnull" json:"name"`
	StrategyType        string             `bun:"strategy_type,notnull" json:"strategyType"`
	Prompt              *string            `bun:"prompt" json:"prompt"`
	CronSchedule        string             `bun:"cron_schedule,notnull" json:"cronSchedule"`
	Enabled             bool               `bun:"enabled,notnull,default:true" json:"enabled"`
	TriggerType         AgentTriggerType   `bun:"trigger_type,notnull,default:'schedule'" json:"triggerType"`
	ReactionConfig      *ReactionConfig    `bun:"reaction_config,type:jsonb" json:"reactionConfig"`
	ExecutionMode       AgentExecutionMode `bun:"execution_mode,default:'execute'" json:"executionMode"`
	Capabilities        *AgentCapabilities `bun:"capabilities,type:jsonb" json:"capabilities"`
	Config              map[string]any     `bun:"config,type:jsonb,default:'{}'" json:"config"`
	Description         *string            `bun:"description" json:"description"`
	LastRunAt           *time.Time         `bun:"last_run_at" json:"lastRunAt"`
	LastRunStatus       *string            `bun:"last_run_status" json:"lastRunStatus"`
	ConsecutiveFailures int                `bun:"consecutive_failures,notnull,default:0" json:"consecutiveFailures"`
	DisabledReason      *string            `bun:"disabled_reason" json:"disabledReason,omitempty"`
	AgentDefinitionID   *string            `bun:"agent_definition_id,type:uuid" json:"agentDefinitionId,omitempty"`
	CreatedAt           time.Time          `bun:"created_at,nullzero,notnull,default:current_timestamp" json:"createdAt"`
	UpdatedAt           time.Time          `bun:"updated_at,nullzero,notnull,default:current_timestamp" json:"updatedAt"`
}

// AgentRun records each execution of an agent for observability
// Table: kb.agent_runs
type AgentRun struct {
	bun.BaseModel `bun:"table:kb.agent_runs,alias:ar"`

	ID           string         `bun:"id,pk,type:uuid,default:gen_random_uuid()" json:"id"`
	AgentID      string         `bun:"agent_id,type:uuid,notnull" json:"agentId"`
	Status       AgentRunStatus `bun:"status,notnull" json:"status"`
	StartedAt    time.Time      `bun:"started_at,notnull" json:"startedAt"`
	LastStepAt   *time.Time     `bun:"last_step_at,type:timestamptz" json:"lastStepAt,omitempty"`
	CompletedAt  *time.Time     `bun:"completed_at" json:"completedAt"`
	DurationMs   *int           `bun:"duration_ms" json:"durationMs"`
	Summary      map[string]any `bun:"summary,type:jsonb,default:'{}'" json:"summary"`
	ErrorMessage *string        `bun:"error_message" json:"errorMessage"`
	SkipReason   *string        `bun:"skip_reason" json:"skipReason"`
	CreatedAt    time.Time      `bun:"created_at,nullzero,notnull,default:current_timestamp" json:"createdAt"`

	// Trigger Source Tracking
	TriggerSource   *string        `bun:"trigger_source" json:"triggerSource"`
	TriggerMetadata map[string]any `bun:"trigger_metadata,type:jsonb" json:"triggerMetadata"`

	// Object-driven work linkage: the work object this run was dispatched for.
	// SubjectObjectID stores the object's canonical_id (the physical id changes
	// on every version).
	SubjectObjectID   *string `bun:"subject_object_id,type:uuid" json:"subjectObjectId,omitempty"`
	SubjectObjectType *string `bun:"subject_object_type" json:"subjectObjectType,omitempty"`
	FailureClass      *string `bun:"failure_class" json:"failureClass,omitempty"`

	// Multi-agent coordination fields
	ParentRunID    *string `bun:"parent_run_id,type:uuid" json:"parentRunId,omitempty"`
	StepCount      int     `bun:"step_count,notnull,default:0" json:"stepCount"`
	MaxSteps       *int    `bun:"max_steps" json:"maxSteps,omitempty"`
	ResumedFrom    *string `bun:"resumed_from,type:uuid" json:"resumedFrom,omitempty"`
	TriggerMessage *string `bun:"trigger_message" json:"triggerMessage,omitempty"`
	Model          *string `bun:"model" json:"model,omitempty"`
	Provider       *string `bun:"provider" json:"provider,omitempty"`
	// ProviderSlug identifies the provider instance that served the run. Null
	// for legacy rows created before instances existed.
	ProviderSlug *string `bun:"provider_slug" json:"providerSlug,omitempty"`
	// Dialect records the provider dialect separately from the instance.
	Dialect *string `bun:"dialect" json:"dialect,omitempty"`

	// Observability linkage: trace_id links the run back to its OTel trace;
	// root_run_id links sub-agent runs back to the top-level orchestration run.
	TraceID   *string `bun:"trace_id" json:"traceId,omitempty"`
	RootRunID *string `bun:"root_run_id,type:uuid" json:"rootRunId,omitempty"`

	// session linkage: optional grouping of runs under a session.
	SessionID *string `bun:"session_id,type:uuid" json:"sessionId,omitempty"`

	AgentDefinitionID *string `bun:"agent_definition_id,type:uuid" json:"agentDefinitionId,omitempty"`

	// TrustedInternal marks a run started through a trusted surface (session UI,
	// scheduler/worker runs, MCP tools, agent→agent delegation). The zero value is
	// false = untrusted/external-facing, so a transport that forgets to declare
	// itself is denied internal agents (fail-closed). It is fixed at run creation
	// and inherited unchanged through delegation and resume, so the internal-agent
	// reachability invariant holds for the whole call chain, not just the first
	// hop (issue #954).
	TrustedInternal bool `bun:"trusted_internal,notnull,default:false" json:"trustedInternal"`

	Tools []string `bun:"tools,array" json:"tools,omitempty"`

	// SuspendContext holds the serialized SuspendSignal when a run is paused via the
	// suspend/resume primitive. Null for runs that were never suspended this way.
	SuspendContext map[string]any `bun:"suspend_context,type:jsonb" json:"suspendContext,omitempty"`

	// Relations
	Agent     *Agent    `bun:"rel:belongs-to,join:agent_id=id" json:"-"`
	ParentRun *AgentRun `bun:"rel:belongs-to,join:parent_run_id=id" json:"-"`
}

// CreateRunOptions holds options for creating an agent run with coordination support
type CreateRunOptions struct {
	AgentID           string
	ParentRunID       *string
	RootRunID         *string // top-level orchestration run ID; persisted at insert when known
	MaxSteps          *int
	ResumedFrom       *string
	InitialStepCount  int // for resumed runs, start from prior run's step_count
	TriggerSource     *string
	TriggerMetadata   map[string]any
	TriggerMessage    *string // optional message injected as user message on wakeup
	Model             *string // model override for this run
	AgentDefinitionID *string
	// TrustedInternal is the fail-closed trust marker persisted on the run row.
	// false (zero value) = external-facing/untrusted; trusted surfaces set true.
	TrustedInternal bool
}

// CreateRunQueuedOptions holds optional parameters for CreateRunQueued.
type CreateRunQueuedOptions struct {
	ParentRunID     *string        // parent run to re-enqueue when this run completes
	RootRunID       *string        // top-level orchestration run ID; inherited by re-enqueued runs
	TriggerMessage  *string        // message injected as user message when worker picks up this run
	TriggerMetadata map[string]any // structured metadata propagated from parent run
	MaxPendingJobs  int            // if > 0, reject the enqueue when the agent already has this many pending jobs
	// TrustedInternal is the fail-closed trust marker persisted on the queued run
	// row and inherited by the worker, so a queued run keeps the trust of the
	// transport that enqueued it (e.g. trigger_agent) rather than being upgraded.
	// false (zero value) = external-facing/untrusted.
	TrustedInternal bool
	// Queue overrides the run's dispatch queue. Empty means resolve from the
	// agent's binding (config override → definition default → "default").
	Queue string
	// Priority orders the dispatch job within its queue; lower is claimed
	// first. Zero means DefaultQueuePriority.
	Priority int
	// SubjectObjectID links the run to the work object it was dispatched for,
	// storing the object's canonical_id. SubjectObjectType records the object
	// type. Set for object-driven (enqueue-on-create) dispatches.
	SubjectObjectID   *string
	SubjectObjectType *string
	// NextRunAt overrides the job's next_run_at (requeue backoff). When set, the
	// job is inserted with this value so a re-enqueued item waits before its next
	// attempt. Zero means "run immediately".
	NextRunAt *time.Time
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
	VisibilityExternal AgentVisibility = "external" // Advertised in the project's A2A agent card and shown in the admin UI
	VisibilityProject  AgentVisibility = "project"  // Shown in the admin UI, not advertised in the A2A agent card
	VisibilityInternal AgentVisibility = "internal" // Hidden from lists; callable only by other agents, never via A2A
)

// NormalizeVisibility trims and lowercases v and maps it to a canonical
// AgentVisibility level. Empty (or whitespace-only) normalizes to project, the
// server default. It returns ok=false for any value that is not one of the
// three levels, so callers never persist an unmappable value (issue #889).
func NormalizeVisibility(v AgentVisibility) (AgentVisibility, bool) {
	switch s := AgentVisibility(strings.ToLower(strings.TrimSpace(string(v)))); s {
	case "":
		return VisibilityProject, true
	case VisibilityExternal, VisibilityProject, VisibilityInternal:
		return s, true
	default:
		return "", false
	}
}

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
	Name string `json:"name,omitempty"`
	// Provider is the provider instance slug that serves Name. When empty, the
	// project's default instance for the dialect is used. This makes the model
	// identity structured (provider + model) rather than a single string.
	Provider    string   `json:"provider,omitempty"`
	Temperature *float32 `json:"temperature,omitempty"`
	MaxTokens   *int     `json:"maxTokens,omitempty"`
	// NativeTools lists Google-native tools to enable when using a Gemini model.
	// Valid values: "google_search", "url_context", "code_execution".
	// Tools are only activated if the selected model actually supports them —
	// unsupported combinations are silently skipped.
	NativeTools []string `json:"nativeTools,omitempty"`
	// EnableThinking controls chain-of-thought reasoning for models that support
	// it (e.g. Qwen3 via OpenAI-compatible endpoint). When nil the provider
	// default applies. Set to false to suppress thinking tokens and improve
	// instruction-following for tool-heavy agents.
	EnableThinking *bool `json:"enableThinking,omitempty"`
}

// ToolPolicy defines a confirmation requirement for a specific tool.
// When Confirm is true, the executor will pause the run before executing
// the tool and ask the user to approve or reject the action.
type ToolPolicy struct {
	// Confirm requires human approval before the tool executes.
	Confirm bool `json:"confirm"`
	// Message is the confirmation prompt shown to the user.
	// Supports template variables: {tool_name}, {args_json}.
	Message string `json:"message,omitempty"`
	// Disabled hard-blocks the tool: the executor returns an error to the
	// agent without calling the tool at all. Use for policy enforcement
	// (e.g. schema_policy=reuse_only blocks finalize-discovery).
	Disabled bool `json:"disabled,omitempty"`
}

// ToolPolicyDefault is the fallback policy applied to tools with no explicit
// ToolPolicies entry on an agent definition.
type ToolPolicyDefault string

const (
	// ToolPolicyDefaultAllow runs tools silently (existing behavior).
	ToolPolicyDefaultAllow ToolPolicyDefault = "allow"
	// ToolPolicyDefaultDeny hard-blocks tools without an explicit entry.
	ToolPolicyDefaultDeny ToolPolicyDefault = "deny"
	// ToolPolicyDefaultAsk requires approval for tools without an explicit entry.
	ToolPolicyDefaultAsk ToolPolicyDefault = "ask"
)

// AgentDefinition stores agent configurations from product manifests.
// This is separate from Agent (which tracks runtime state like last_run_at).
// Table: kb.agent_definitions
// AgentWorkStatusConfig maps work-item lifecycle phases to object status values.
type AgentWorkStatusConfig struct {
	Ready      string `json:"ready"`
	InProgress string `json:"inProgress"`
	Review     string `json:"review"`
	Revision   string `json:"revision"`
	Blocked    string `json:"blocked"`
	Done       string `json:"done"`
}

// AgentRetryPolicy configures retry backoff for queued work runs.
type AgentRetryPolicy struct {
	MaxAttempts        int     `json:"maxAttempts"`
	InitialIntervalMS  int     `json:"initialIntervalMs"`
	BackoffCoefficient float64 `json:"backoffCoefficient"`
	MaxIntervalMS      int     `json:"maxIntervalMs"`
}

// AgentWorkContract declares the deliverables an agent commits to producing
// before it may complete a work item (P6). A zero value (both fields empty)
// means "no validation": work_complete behaves exactly as before.
type AgentWorkContract struct {
	// RequireArtifacts requires work_complete to carry a non-empty summary or
	// artifacts before the item may be completed.
	RequireArtifacts bool `json:"requireArtifacts"`
	// RequiredDeliverableTypes lists object types that must be among the
	// declared deliverables, each resolving to an existing object, before the
	// item may be completed. Empty means no required deliverable types.
	RequiredDeliverableTypes []string `json:"requiredDeliverableTypes,omitempty"`
}

// IsZero reports whether the contract carries no validation requirements.
func (c AgentWorkContract) IsZero() bool {
	return !c.RequireArtifacts && len(c.RequiredDeliverableTypes) == 0
}

// AgentWorkConfig is the object-driven work configuration on an agent
// definition. A zero value means "use defaults".
type AgentWorkConfig struct {
	Status         AgentWorkStatusConfig `json:"status"`
	RequiresReview bool                  `json:"requiresReview"`
	FailureLimit   int                   `json:"failureLimit"`
	// RevisionLimit caps the number of rework (request-changes) rounds before
	// the item is escalated to a human instead of re-enqueued. Zero = default.
	RevisionLimit int `json:"revisionLimit"`
	// RetryPolicy configures retry backoff for queued work runs.
	RetryPolicy AgentRetryPolicy `json:"retryPolicy"`
	// WorkContract declares the deliverables the agent must produce before it
	// may complete a work item (P6). Zero value = no validation. It is
	// agent-only (no per-type override): the required deliverable types describe
	// what the *agent* produces, whereas the per-type config describes how items
	// of a type are processed (see design.md "Work contract").
	WorkContract AgentWorkContract `json:"workContract"`
}

// IsZero reports whether the work config carries no explicit configuration, i.e.
// the definition does not opt into object-driven work (today's inline behaviour
// is preserved). A non-zero config (or a queued dispatch mode) is what gates
// enqueue-on-create.
func (w AgentWorkConfig) IsZero() bool {
	return w.Status == (AgentWorkStatusConfig{}) &&
		!w.RequiresReview &&
		w.FailureLimit == 0 &&
		w.RevisionLimit == 0 &&
		w.RetryPolicy == (AgentRetryPolicy{}) &&
		w.WorkContract.IsZero()
}

// ReadyStatus returns the configured "ready" work-status value, defaulting to
// the built-in "ready" when unset.
func (w AgentWorkConfig) ReadyStatus() string {
	if w.Status.Ready != "" {
		return w.Status.Ready
	}
	return "ready"
}

// InProgressStatus returns the configured "in_progress" work-status value,
// defaulting to the built-in "in_progress" when unset.
func (w AgentWorkConfig) InProgressStatus() string {
	if w.Status.InProgress != "" {
		return w.Status.InProgress
	}
	return "in_progress"
}

// ReviewStatus returns the configured "review" work-status value, defaulting to
// the built-in "review" when unset.
func (w AgentWorkConfig) ReviewStatus() string {
	if w.Status.Review != "" {
		return w.Status.Review
	}
	return "review"
}

// RevisionStatus returns the configured "revision" work-status value, defaulting
// to the built-in "revision" when unset.
func (w AgentWorkConfig) RevisionStatus() string {
	if w.Status.Revision != "" {
		return w.Status.Revision
	}
	return "revision"
}

// BlockedStatus returns the configured "blocked" work-status value, defaulting
// to the built-in "blocked" when unset.
func (w AgentWorkConfig) BlockedStatus() string {
	if w.Status.Blocked != "" {
		return w.Status.Blocked
	}
	return "blocked"
}

// DoneStatus returns the configured "done" work-status value, defaulting to the
// built-in "done" when unset.
func (w AgentWorkConfig) DoneStatus() string {
	if w.Status.Done != "" {
		return w.Status.Done
	}
	return "done"
}

// FailureLimitValue returns the configured per-item failure budget, defaulting
// to the built-in default (3) when unset.
func (w AgentWorkConfig) FailureLimitValue() int {
	if w.FailureLimit > 0 {
		return w.FailureLimit
	}
	return defaultWorkFailureLimit
}

// RevisionLimitValue returns the configured rework (revision) cap, defaulting
// to the built-in default (3) when unset.
func (w AgentWorkConfig) RevisionLimitValue() int {
	if w.RevisionLimit > 0 {
		return w.RevisionLimit
	}
	return defaultWorkRevisionLimit
}

type AgentDefinition struct {
	bun.BaseModel `bun:"table:kb.agent_definitions,alias:ad"`

	ID               string            `bun:"id,pk,type:uuid,default:gen_random_uuid()" json:"id"`
	ProductID        *string           `bun:"product_id,type:uuid" json:"productId,omitempty"`
	ProjectID        string            `bun:"project_id,type:uuid,notnull" json:"projectId"`
	Name             string            `bun:"name,notnull" json:"name"`
	Description      *string           `bun:"description" json:"description,omitempty"`
	SystemPrompt     *string           `bun:"system_prompt" json:"systemPrompt,omitempty"`
	Model            *ModelConfig      `bun:"model,type:jsonb,default:'{}'" json:"model,omitempty"`
	Tools            []string          `bun:"tools,array" json:"tools"`
	BannedTools      []string          `bun:"banned_tools,array" json:"bannedTools,omitempty"`
	Skills           []string          `bun:"skills,array" json:"skills"`
	AutoLoadSkills   bool              `bun:"auto_load_skills,notnull,default:false" json:"autoLoadSkills"`
	FlowType         AgentFlowType     `bun:"flow_type,notnull,default:'single'" json:"flowType"`
	IsDefault        bool              `bun:"is_default,notnull,default:false" json:"isDefault"`
	Enabled          bool              `bun:"enabled,notnull,default:true" json:"enabled"`
	MaxSteps         *int              `bun:"max_steps" json:"maxSteps,omitempty"`
	DefaultTimeout   *int              `bun:"default_timeout" json:"defaultTimeout,omitempty"`
	MaxSessionEvents *int              `bun:"max_session_events" json:"maxSessionEvents,omitempty"`
	Visibility       AgentVisibility   `bun:"visibility,notnull,default:'project'" json:"visibility"`
	ACPConfig        *ACPConfig        `bun:"acp_config,type:jsonb" json:"acpConfig,omitempty"`
	Config           map[string]any    `bun:"config,type:jsonb,default:'{}'" json:"config,omitempty"`
	UIConfig         json.RawMessage   `bun:"ui_config,type:jsonb,default:'{}'" json:"uiConfig,omitempty"`
	SandboxConfig    map[string]any    `bun:"sandbox_config,type:jsonb" json:"sandboxConfig,omitempty"`
	DispatchMode     AgentDispatchMode `bun:"dispatch_mode,notnull,default:'sync'" json:"dispatchMode"`
	// DefaultQueue binds this definition to a named work queue. Runs of a
	// runtime agent instantiated from this definition are enqueued on this
	// queue unless the runtime agent's Config overrides it with "queue".
	DefaultQueue string `bun:"default_queue,notnull,default:'default'" json:"defaultQueue"`
	// WorkConfig configures object-driven work: the status phase mapping,
	// requiresReview, failureLimit, and retryPolicy. Zero value = defaults.
	WorkConfig AgentWorkConfig `bun:"work_config,type:jsonb,notnull,default:'{}'" json:"workConfig"`
	// ToolPolicies maps tool name → policy. When a tool has Confirm:true,
	// the executor pauses the run and asks the user before executing the tool.
	ToolPolicies map[string]ToolPolicy `bun:"tool_policies,type:jsonb,default:'{}'" json:"toolPolicies,omitempty"`
	// DefaultToolPolicy is the fallback policy for tools without an explicit
	// ToolPolicies entry. "allow" (default) preserves existing behavior; "deny"
	// blocks unlisted tools; "ask" requires confirmation for them.
	DefaultToolPolicy ToolPolicyDefault `bun:"default_tool_policy,notnull,default:'allow'" json:"defaultToolPolicy,omitempty"`
	// SourceBlueprintID is the ID of the blueprint that created this definition
	// (NULL for manual/pre-existing definitions). Unapply uses it to delete only
	// agents the blueprint actually created.
	SourceBlueprintID *string   `bun:"source_blueprint_id,type:uuid" json:"sourceBlueprintId,omitempty"`
	CreatedAt         time.Time `bun:"created_at,nullzero,notnull,default:current_timestamp" json:"createdAt"`
	UpdatedAt         time.Time `bun:"updated_at,nullzero,notnull,default:current_timestamp" json:"updatedAt"`
}

// toolGroupPolicyPrefix is the reserved key prefix for group-level tool
// policies stored in AgentDefinition.ToolPolicies. Tool names are validated
// identifiers that cannot begin with '@', so the prefix cannot collide with a
// real tool name.
const toolGroupPolicyPrefix = "@group:"

// effectiveToolPolicy is the catalog-less wrapper for effectiveToolPolicyFor. It
// resolves the tool's scope via the static mcp.LookupToolScope map (static core
// tools only), falling back to the unscoped static map for workspace/web tools.
// Enforcement callers that already hold the tool catalog should call
// effectiveToolPolicyFor with the catalog's RequiredScope so dynamic tools
// resolve to the same group the read DTO reports.
func (d *AgentDefinition) effectiveToolPolicy(toolName string) (ToolPolicy, bool) {
	scope, _ := mcp.LookupToolScope(toolName)
	return d.effectiveToolPolicyFor(toolName, scope)
}

// effectiveToolPolicyFor returns the policy that governs toolName, resolved in
// the order: explicit ToolPolicies[toolName] → group policy
// ToolPolicies["@group:<id>"] (where <id> = toolgroups.GroupForScope(scope,
// toolName)) → DefaultToolPolicy. The group lookup is skipped when the resolved
// group is GroupOther: `other` is display-only and never a policy source, so an
// external/relay/unmatched tool falls through to DefaultToolPolicy. An empty
// scope degrades to the unscoped static mapping. The bool is false only for the
// "allow / non-disabled" default result; an explicit or group entry (including
// an empty `{}` entry) returns true.
func (d *AgentDefinition) effectiveToolPolicyFor(toolName, scope string) (ToolPolicy, bool) {
	if p, ok := d.ToolPolicies[toolName]; ok {
		return p, true
	}
	g := toolgroups.GroupForScope(scope, toolName)
	if g != toolgroups.GroupOther {
		if p, ok := d.ToolPolicies[toolGroupPolicyPrefix+g]; ok {
			return p, true
		}
	}
	switch d.DefaultToolPolicy {
	case ToolPolicyDefaultDeny:
		return ToolPolicy{Disabled: true}, true
	case ToolPolicyDefaultAsk:
		return ToolPolicy{Confirm: true}, true
	default:
		return ToolPolicy{}, false
	}
}

// toolPolicyBlocks reports whether the resolved tool policy hard-blocks a tool
// before execution (the executor's beforeToolCb deny chokepoint). It is the
// single source of truth for the disabled check so the executor and its tests
// cannot drift.
func toolPolicyBlocks(policy ToolPolicy, hasPolicy bool) bool {
	return hasPolicy && policy.Disabled
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

// AgentQuestionStatus defines the lifecycle status of an agent question
type AgentQuestionStatus string

const (
	QuestionStatusPending   AgentQuestionStatus = "pending"
	QuestionStatusAnswered  AgentQuestionStatus = "answered"
	QuestionStatusExpired   AgentQuestionStatus = "expired"
	QuestionStatusCancelled AgentQuestionStatus = "cancelled"
)

// AgentQuestionInteractionType defines how a question should be rendered on the client.
type AgentQuestionInteractionType string

const (
	// QuestionInteractionButtons presents options as clickable buttons (default for 2-5 options).
	QuestionInteractionButtons AgentQuestionInteractionType = "buttons"
	// QuestionInteractionSelect presents options as a dropdown menu (for 5+ options).
	QuestionInteractionSelect AgentQuestionInteractionType = "select"
	// QuestionInteractionMultiSelect presents options as a multi-select dropdown.
	QuestionInteractionMultiSelect AgentQuestionInteractionType = "multi_select"
	// QuestionInteractionText opens a free-text input modal (no options needed).
	QuestionInteractionText AgentQuestionInteractionType = "text"
)

// AgentQuestionOption represents a single option in an ask_user question.
type AgentQuestionOption struct {
	Label       string `json:"label"`
	Value       string `json:"value"`
	Description string `json:"description,omitempty"`
}

// AgentQuestion represents a question posed by an agent to a user during execution.
// Table: kb.agent_questions
type AgentQuestion struct {
	bun.BaseModel `bun:"table:kb.agent_questions,alias:aq"`

	ID        string `bun:"id,pk,type:uuid,default:gen_random_uuid()" json:"id"`
	RunID     string `bun:"run_id,type:uuid,notnull" json:"runId"`
	AgentID   string `bun:"agent_id,type:uuid,notnull" json:"agentId"`
	ProjectID string `bun:"project_id,type:uuid,notnull" json:"projectId"`
	Question  string `bun:"question,notnull" json:"question"`
	// Proposal is an optional structured payload attached to an ask_user
	// checkpoint. The envelope shape is {kind, summary, body}; kind is a string
	// discriminator (e.g. "blueprint") and body holds the kind-specific manifest.
	// Nil for plain-text questions.
	Proposal        map[string]any               `bun:"proposal,type:jsonb" json:"proposal,omitempty"`
	Options         []AgentQuestionOption        `bun:"options,type:jsonb,notnull,default:'[]'" json:"options"`
	InteractionType AgentQuestionInteractionType `bun:"interaction_type,type:text,notnull,default:'buttons'" json:"interactionType"`
	Placeholder     string                       `bun:"placeholder,type:text" json:"placeholder,omitempty"`
	MaxLength       int                          `bun:"max_length" json:"maxLength,omitempty"`
	Response        *string                      `bun:"response" json:"response,omitempty"`
	RespondedBy     *string                      `bun:"responded_by,type:uuid" json:"respondedBy,omitempty"`
	RespondedAt     *time.Time                   `bun:"responded_at" json:"respondedAt,omitempty"`
	Status          AgentQuestionStatus          `bun:"status,notnull,default:'pending'" json:"status"`
	NotificationID  *string                      `bun:"notification_id,type:uuid" json:"notificationId,omitempty"`
	CreatedAt       time.Time                    `bun:"created_at,nullzero,notnull,default:current_timestamp" json:"createdAt"`
	UpdatedAt       time.Time                    `bun:"updated_at,nullzero,notnull,default:current_timestamp" json:"updatedAt"`

	// Relations
	Run   *AgentRun `bun:"rel:belongs-to,join:run_id=id" json:"-"`
	Agent *Agent    `bun:"rel:belongs-to,join:agent_id=id" json:"-"`
}

// AgentToolApproval records a single human decision on a tool-policy
// confirmation (approve / reject / cancel). It is the audit trail for
// human-in-the-loop tool gating.
// Table: kb.agent_tool_approvals
type AgentToolApproval struct {
	bun.BaseModel `bun:"table:kb.agent_tool_approvals,alias:ata"`

	ID          string         `bun:"id,pk,type:uuid,default:gen_random_uuid()" json:"id"`
	RunID       string         `bun:"run_id,type:uuid,notnull" json:"runId"`
	ProjectID   string         `bun:"project_id,type:uuid,notnull" json:"projectId"`
	AgentID     string         `bun:"agent_id,type:uuid,notnull" json:"agentId"`
	QuestionID  string         `bun:"question_id,type:uuid,notnull" json:"questionId"`
	ToolName    string         `bun:"tool_name,notnull" json:"toolName"`
	ArgsSummary map[string]any `bun:"args_summary,type:jsonb,notnull,default:'{}'" json:"argsSummary"`
	Decision    string         `bun:"decision,notnull,default:'pending'" json:"decision"` // pending, approved, rejected, cancelled
	Message     string         `bun:"message,type:text" json:"message,omitempty"`
	DecidedBy   *string        `bun:"decided_by,type:uuid" json:"decidedBy,omitempty"`
	DecidedAt   *time.Time     `bun:"decided_at" json:"decidedAt,omitempty"`
	CreatedAt   time.Time      `bun:"created_at,nullzero,notnull,default:current_timestamp" json:"createdAt"`
	UpdatedAt   time.Time      `bun:"updated_at,nullzero,notnull,default:current_timestamp" json:"updatedAt"`

	// ShareLinkID is the public agent-share link this approval's run belongs to
	// (nullable — nil for non-share runs). Added by migration 00160.
	ShareLinkID *string `bun:"share_link_id,type:uuid" json:"shareLinkId,omitempty"`

	// ConversationID is the chat conversation this approval's run belongs to,
	// resolved via agent_runs.session_id → chat_conversations.id. Populated
	// only by ListToolApprovals (not a stored column).
	ConversationID *string `bun:"-" json:"conversationId,omitempty"`
}

// AgentJobStatus defines the status of an agent run job in the dispatch queue.
type AgentJobStatus string

const (
	JobStatusPending    AgentJobStatus = "pending"
	JobStatusProcessing AgentJobStatus = "processing"
	JobStatusCompleted  AgentJobStatus = "completed"
	JobStatusFailed     AgentJobStatus = "failed"
)

// AgentRunJob is the dispatch ledger entry for a queued agent run.
// Workers claim rows using FOR UPDATE SKIP LOCKED.
// Table: kb.agent_run_jobs
type AgentRunJob struct {
	bun.BaseModel `bun:"table:kb.agent_run_jobs,alias:arj"`

	ID           string         `bun:"id,pk,type:uuid,default:gen_random_uuid()" json:"id"`
	RunID        string         `bun:"run_id,type:uuid,notnull" json:"runId"`
	Status       AgentJobStatus `bun:"status,notnull,default:'pending'" json:"status"`
	AttemptCount int            `bun:"attempt_count,notnull,default:0" json:"attemptCount"`
	MaxAttempts  int            `bun:"max_attempts,notnull,default:1" json:"maxAttempts"`
	// Queue is the named work queue this job is claimed from. Defaults to
	// DefaultQueueName when the run's agent has no queue binding.
	Queue string `bun:"queue,notnull,default:'default'" json:"queue"`
	// Priority orders claims within a queue; lower is claimed first.
	Priority int `bun:"priority,notnull,default:100" json:"priority"`
	// ProjectID scopes the queue: queue names are unique per project, so a
	// claim must match both project and queue.
	ProjectID   *string    `bun:"project_id,type:uuid" json:"projectId,omitempty"`
	NextRunAt   time.Time  `bun:"next_run_at,notnull,default:now()" json:"nextRunAt"`
	CreatedAt   time.Time  `bun:"created_at,nullzero,notnull,default:current_timestamp" json:"createdAt"`
	CompletedAt *time.Time `bun:"completed_at" json:"completedAt,omitempty"`

	// Relations
	Run *AgentRun `bun:"rel:belongs-to,join:run_id=id" json:"-"`
}

// DefaultQueueName is the queue every run falls back to when its agent has no
// queue binding.
const DefaultQueueName = "default"

// DefaultQueuePriority is the priority assigned to a job when none is set.
const DefaultQueuePriority = 100

// AgentQueue is a named, project-scoped agent work queue. Jobs are routed to a
// queue based on the triggering agent's binding; workers claim per queue.
// Table: kb.agent_queues
type AgentQueue struct {
	bun.BaseModel `bun:"table:kb.agent_queues,alias:aq"`

	ProjectID   string    `bun:"project_id,pk,type:uuid" json:"projectId"`
	Name        string    `bun:"name,pk" json:"name"`
	DisplayName string    `bun:"display_name,notnull,default:''" json:"displayName"`
	Description string    `bun:"description,notnull,default:''" json:"description"`
	Concurrency int       `bun:"concurrency,notnull,default:1" json:"concurrency"`
	Priority    int       `bun:"priority,notnull,default:100" json:"priority"`
	Enabled     bool      `bun:"enabled,notnull,default:true" json:"enabled"`
	CreatedAt   time.Time `bun:"created_at,nullzero,notnull,default:current_timestamp" json:"createdAt"`
	UpdatedAt   time.Time `bun:"updated_at,nullzero,notnull,default:current_timestamp" json:"updatedAt"`

	// Depth is populated by listing queries; not persisted.
	Pending    int `bun:"-" json:"pending"`
	Processing int `bun:"-" json:"processing"`
}

// Session represents a thin session grouping for runs.
// Sessions track run history only — no cross-run context injection.
// Table: kb.sessions
type Session struct {
	bun.BaseModel `bun:"table:kb.sessions,alias:acps"`

	ID        string    `bun:"id,pk,type:uuid,default:gen_random_uuid()" json:"id"`
	ProjectID string    `bun:"project_id,type:uuid,notnull" json:"projectId"`
	AgentName *string   `bun:"agent_name" json:"agentName,omitempty"`
	Title     *string   `bun:"title" json:"title,omitempty"`
	CreatedAt time.Time `bun:"created_at,nullzero,notnull,default:current_timestamp" json:"createdAt"`
	UpdatedAt time.Time `bun:"updated_at,nullzero,notnull,default:current_timestamp" json:"updatedAt"`
}

// RunEvent represents a persisted run event emitted during an agent run.
// Replayed by the A2A v1.0 SubscribeTask to reconstruct a task's event stream.
// Table: kb.run_events
type RunEvent struct {
	bun.BaseModel `bun:"table:kb.run_events,alias:acre"`

	ID        string         `bun:"id,pk,type:uuid,default:gen_random_uuid()" json:"id"`
	RunID     string         `bun:"run_id,type:uuid,notnull" json:"runId"`
	EventType string         `bun:"event_type,notnull" json:"eventType"`
	Data      map[string]any `bun:"data,type:jsonb,notnull,default:'{}'" json:"data"`
	CreatedAt time.Time      `bun:"created_at,nullzero,notnull,default:current_timestamp" json:"createdAt"`

	// Relations
	Run *AgentRun `bun:"rel:belongs-to,join:run_id=id" json:"-"`
}

// run event types persisted in kb.run_events.event_type. Reused by the
// A2A v1.0 SubscribeTask replay for task-state mapping and stream
// reconstruction.
const (
	ACPEventRunCreated    = "run.created"
	ACPEventRunInProgress = "run.in-progress"
	ACPEventRunAwaiting   = "run.awaiting"
	ACPEventRunCompleted  = "run.completed"
	ACPEventRunFailed     = "run.failed"
	ACPEventRunCancelled  = "run.cancelled"
	ACPEventMessagePart   = "message.part"
	ACPEventError         = "error"
	ACPEventToolCall      = "tool_call"
	ACPEventToolResult    = "tool_result"
)
