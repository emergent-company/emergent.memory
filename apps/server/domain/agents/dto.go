package agents

import "time"

// AgentDTO is the response DTO for an agent
type AgentDTO struct {
	ID                  string             `json:"id"`
	ProjectID           string             `json:"projectId"`
	Name                string             `json:"name"`
	StrategyType        string             `json:"strategyType"`
	Prompt              *string            `json:"prompt"`
	CronSchedule        string             `json:"cronSchedule"`
	Enabled             bool               `json:"enabled"`
	TriggerType         AgentTriggerType   `json:"triggerType"`
	ReactionConfig      *ReactionConfig    `json:"reactionConfig"`
	ExecutionMode       AgentExecutionMode `json:"executionMode"`
	Capabilities        *AgentCapabilities `json:"capabilities"`
	Config              map[string]any     `json:"config"`
	Description         *string            `json:"description"`
	LastRunAt           *time.Time         `json:"lastRunAt"`
	LastRunStatus       *string            `json:"lastRunStatus"`
	ConsecutiveFailures int                `json:"consecutiveFailures"`
	CreatedAt           time.Time          `json:"createdAt"`
	UpdatedAt           time.Time          `json:"updatedAt"`
}

// AgentRunDTO is the response DTO for an agent run
type AgentRunDTO struct {
	ID            string         `json:"id"`
	AgentID       string         `json:"agentId"`
	AgentName     string         `json:"agentName,omitempty"`
	Status        AgentRunStatus `json:"status"`
	SessionStatus SessionStatus  `json:"sessionStatus"`
	StartedAt     time.Time      `json:"startedAt"`
	CompletedAt   *time.Time     `json:"completedAt"`
	DurationMs    *int           `json:"durationMs"`
	Summary       map[string]any `json:"summary"`
	ErrorMessage  *string        `json:"errorMessage"`
	SkipReason    *string        `json:"skipReason"`

	// Multi-agent coordination fields
	ParentRunID *string `json:"parentRunId,omitempty"`
	StepCount   int     `json:"stepCount"`
	MaxSteps    *int    `json:"maxSteps,omitempty"`
	ResumedFrom *string `json:"resumedFrom,omitempty"`

	// Observability linkage
	TraceID   *string `json:"traceId,omitempty"`
	RootRunID *string `json:"rootRunId,omitempty"`

	// Model used for this run (resolved at execution time)
	Model string `json:"model,omitempty"`

	// Token usage aggregated from kb.llm_usage_events for this run.
	TokenUsage *RunTokenUsage `json:"tokenUsage,omitempty"`

	// Workspace/sandbox details for this run (when applicable).
	Workspace *RunWorkspaceDTO `json:"workspace,omitempty"`
}

// RunWorkspaceDTO exposes sandbox/workspace details on a run response.
type RunWorkspaceDTO struct {
	Provider    string `json:"provider"`
	ContainerID string `json:"containerId,omitempty"`
	BaseImage   string `json:"baseImage,omitempty"`
	ImageDigest string `json:"imageDigest,omitempty"`
}

// RunTokenUsage holds aggregated LLM token counts and estimated cost for a run.
type RunTokenUsage struct {
	TotalInputTokens  int64   `json:"totalInputTokens"`
	TotalOutputTokens int64   `json:"totalOutputTokens"`
	EstimatedCostUSD  float64 `json:"estimatedCostUsd"`
	// Provider and Model identify the LLM used. Format: "<provider>/<model>",
	// e.g. "google/gemini-2.0-flash". Empty when unknown.
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
}

// CreateAgentDTO is the request DTO for creating an agent
type CreateAgentDTO struct {
	ProjectID      string             `json:"projectId" validate:"required,uuid"`
	Name           string             `json:"name" validate:"required"`
	StrategyType   string             `json:"strategyType" validate:"required"`
	Prompt         *string            `json:"prompt"`
	CronSchedule   string             `json:"cronSchedule" validate:"required"`
	Enabled        *bool              `json:"enabled"`
	TriggerType    AgentTriggerType   `json:"triggerType"`
	ReactionConfig *ReactionConfig    `json:"reactionConfig"`
	ExecutionMode  AgentExecutionMode `json:"executionMode"`
	Capabilities   *AgentCapabilities `json:"capabilities"`
	Config         map[string]any     `json:"config"`
	Description    *string            `json:"description"`
}

// UpdateAgentDTO is the request DTO for updating an agent
type UpdateAgentDTO struct {
	Name           *string             `json:"name"`
	Prompt         *string             `json:"prompt"`
	Enabled        *bool               `json:"enabled"`
	CronSchedule   *string             `json:"cronSchedule"`
	TriggerType    *AgentTriggerType   `json:"triggerType"`
	ReactionConfig *ReactionConfig     `json:"reactionConfig"`
	ExecutionMode  *AgentExecutionMode `json:"executionMode"`
	Capabilities   *AgentCapabilities  `json:"capabilities"`
	Config         map[string]any      `json:"config"`
	Description    *string             `json:"description"`
}

// BatchTriggerDTO is the request DTO for batch triggering an agent
type BatchTriggerDTO struct {
	ObjectIDs []string `json:"objectIds" validate:"required,min=1,max=100,dive,uuid"`
}

// PendingEventObjectDTO represents a graph object pending processing
type PendingEventObjectDTO struct {
	ID        string    `json:"id"`
	Type      string    `json:"type"`
	Key       string    `json:"key"`
	Version   int       `json:"version"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// PendingEventsResponseDTO is the response for pending events query
type PendingEventsResponseDTO struct {
	TotalCount     int                     `json:"totalCount"`
	Objects        []PendingEventObjectDTO `json:"objects"`
	ReactionConfig struct {
		ObjectTypes []string `json:"objectTypes"`
		Events      []string `json:"events"`
	} `json:"reactionConfig"`
}

// BatchTriggerResponseDTO is the response for batch trigger
type BatchTriggerResponseDTO struct {
	Queued         int `json:"queued"`
	Skipped        int `json:"skipped"`
	SkippedDetails []struct {
		ObjectID string `json:"objectId"`
		Reason   string `json:"reason"`
	} `json:"skippedDetails"`
}

// TriggerRequestDTO is the request body for triggering an agent
type TriggerRequestDTO struct {
	Prompt  string            `json:"prompt"`
	Context map[string]any    `json:"context,omitempty"`
	Model   string            `json:"model,omitempty"`
	EnvVars map[string]string `json:"env_vars,omitempty"`
}

// TriggerResponseDTO is the response for triggering an agent
type TriggerResponseDTO struct {
	Success bool    `json:"success"`
	RunID   *string `json:"runId,omitempty"`
	Message *string `json:"message,omitempty"`
	Error   *string `json:"error,omitempty"`
}

// APIResponse wraps API responses with success flag
type APIResponse[T any] struct {
	Success bool    `json:"success"`
	Data    T       `json:"data,omitempty"`
	Error   *string `json:"error,omitempty"`
	Message *string `json:"message,omitempty"`
}

// ToDTO converts an Agent entity to AgentDTO
func (a *Agent) ToDTO() *AgentDTO {
	return &AgentDTO{
		ID:                  a.ID,
		ProjectID:           a.ProjectID,
		Name:                a.Name,
		StrategyType:        a.StrategyType,
		Prompt:              a.Prompt,
		CronSchedule:        a.CronSchedule,
		Enabled:             a.Enabled,
		TriggerType:         a.TriggerType,
		ReactionConfig:      a.ReactionConfig,
		ExecutionMode:       a.ExecutionMode,
		Capabilities:        a.Capabilities,
		Config:              a.Config,
		Description:         a.Description,
		LastRunAt:           a.LastRunAt,
		LastRunStatus:       a.LastRunStatus,
		ConsecutiveFailures: a.ConsecutiveFailures,
		CreatedAt:           a.CreatedAt,
		UpdatedAt:           a.UpdatedAt,
	}
}

// ToDTO converts an AgentRun entity to AgentRunDTO
func (r *AgentRun) ToDTO() *AgentRunDTO {
	dto := &AgentRunDTO{
		ID:            r.ID,
		AgentID:       r.AgentID,
		Status:        r.Status,
		SessionStatus: r.SessionStatus,
		StartedAt:     r.StartedAt,
		CompletedAt:   r.CompletedAt,
		DurationMs:    r.DurationMs,
		Summary:       r.Summary,
		ErrorMessage:  r.ErrorMessage,
		SkipReason:    r.SkipReason,
		ParentRunID:   r.ParentRunID,
		StepCount:     r.StepCount,
		MaxSteps:      r.MaxSteps,
		ResumedFrom:   r.ResumedFrom,
		TraceID:       r.TraceID,
		RootRunID:     r.RootRunID,
	}
	if r.Agent != nil {
		dto.AgentName = r.Agent.Name
	}
	if r.Model != nil {
		dto.Model = *r.Model
	}
	return dto
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

// PaginatedResponse wraps paginated API responses
type PaginatedResponse[T any] struct {
	Items      []T `json:"items"`
	TotalCount int `json:"totalCount"`
	Limit      int `json:"limit"`
	Offset     int `json:"offset"`
}

// AgentWithDefinitionDTO enriches AgentDTO with fields from the agent's definition.
// Returned by list_agents MCP tool so callers get name, description, and config
// in a single call without a separate list_agent_definitions lookup.
type AgentWithDefinitionDTO struct {
	// Runtime fields (from kb.agents)
	ID            string             `json:"id"`
	ProjectID     string             `json:"projectId"`
	Name          string             `json:"name"`
	Enabled       bool               `json:"enabled"`
	TriggerType   AgentTriggerType   `json:"triggerType"`
	ExecutionMode AgentExecutionMode `json:"executionMode"`
	LastRunAt     *time.Time         `json:"lastRunAt,omitempty"`
	LastRunStatus *string            `json:"lastRunStatus,omitempty"`

	// Definition fields (from kb.agent_definitions, may be nil if no definition)
	Description *string       `json:"description,omitempty"`
	FlowType    AgentFlowType `json:"flowType,omitempty"`
	Model       *ModelConfig  `json:"model,omitempty"`
	AgentType   *string       `json:"agentType,omitempty"`
	Tier        *string       `json:"tier,omitempty"`
}

// --- Agent Definition DTOs ---

// AgentDefinitionDTO is the full response DTO for an agent definition
type AgentDefinitionDTO struct {
	ID             string            `json:"id"`
	ProductID      *string           `json:"productId,omitempty"`
	ProjectID      string            `json:"projectId"`
	Name           string            `json:"name"`
	Description    *string           `json:"description,omitempty"`
	SystemPrompt   *string           `json:"systemPrompt,omitempty"`
	Model          *ModelConfig      `json:"model,omitempty"`
	Tools          []string          `json:"tools"`
	FlowType       AgentFlowType     `json:"flowType"`
	IsDefault      bool              `json:"isDefault"`
	MaxSteps       *int              `json:"maxSteps,omitempty"`
	DefaultTimeout *int              `json:"defaultTimeout,omitempty"`
	Visibility     AgentVisibility   `json:"visibility"`
	DispatchMode   AgentDispatchMode `json:"dispatchMode"`
	ACPConfig      *ACPConfig        `json:"acpConfig,omitempty"`
	Config         map[string]any    `json:"config,omitempty"`
	SandboxConfig  map[string]any    `json:"workspaceConfig,omitempty"`
	CreatedAt      time.Time         `json:"createdAt"`
	UpdatedAt      time.Time         `json:"updatedAt"`
}

// AgentDefinitionSummaryDTO is a lightweight DTO for listing agent definitions
type AgentDefinitionSummaryDTO struct {
	ID               string          `json:"id"`
	ProjectID        string          `json:"projectId"`
	Name             string          `json:"name"`
	Description      *string         `json:"description,omitempty"`
	FlowType         AgentFlowType   `json:"flowType"`
	Visibility       AgentVisibility `json:"visibility"`
	IsDefault        bool            `json:"isDefault"`
	ToolCount        int             `json:"toolCount"`
	HasSandboxConfig bool            `json:"hasSandboxConfig"`
	CreatedAt        time.Time       `json:"createdAt"`
	UpdatedAt        time.Time       `json:"updatedAt"`
}

// CreateAgentDefinitionDTO is the request DTO for creating an agent definition
type CreateAgentDefinitionDTO struct {
	Name           string            `json:"name" validate:"required"`
	Description    *string           `json:"description"`
	SystemPrompt   *string           `json:"systemPrompt"`
	Model          *ModelConfig      `json:"model"`
	Tools          []string          `json:"tools"`
	Skills         []string          `json:"skills"`
	FlowType       AgentFlowType     `json:"flowType"`
	IsDefault      *bool             `json:"isDefault"`
	MaxSteps       *int              `json:"maxSteps"`
	DefaultTimeout *int              `json:"defaultTimeout"`
	Visibility     AgentVisibility   `json:"visibility"`
	DispatchMode   AgentDispatchMode `json:"dispatchMode"`
	ACPConfig      *ACPConfig        `json:"acpConfig"`
	Config         map[string]any    `json:"config"`
	SandboxConfig  map[string]any    `json:"workspaceConfig"`
}

// UpdateAgentDefinitionDTO is the request DTO for updating an agent definition
type UpdateAgentDefinitionDTO struct {
	Name           *string            `json:"name"`
	Description    *string            `json:"description"`
	SystemPrompt   *string            `json:"systemPrompt"`
	Model          *ModelConfig       `json:"model"`
	Tools          []string           `json:"tools"`
	Skills         []string           `json:"skills"`
	FlowType       *AgentFlowType     `json:"flowType"`
	IsDefault      *bool              `json:"isDefault"`
	MaxSteps       *int               `json:"maxSteps"`
	DefaultTimeout *int               `json:"defaultTimeout"`
	Visibility     *AgentVisibility   `json:"visibility"`
	DispatchMode   *AgentDispatchMode `json:"dispatchMode"`
	ACPConfig      *ACPConfig         `json:"acpConfig"`
	Config         map[string]any     `json:"config"`
	SandboxConfig  map[string]any     `json:"workspaceConfig"`
}

// --- Agent Run Message / Tool Call DTOs ---

// AgentRunMessageDTO is the response DTO for an agent run message
type AgentRunMessageDTO struct {
	ID         string         `json:"id"`
	RunID      string         `json:"runId"`
	Role       string         `json:"role"`
	Content    map[string]any `json:"content"`
	StepNumber int            `json:"stepNumber"`
	CreatedAt  time.Time      `json:"createdAt"`
}

// AgentRunToolCallDTO is the response DTO for an agent run tool call
type AgentRunToolCallDTO struct {
	ID         string         `json:"id"`
	RunID      string         `json:"runId"`
	MessageID  *string        `json:"messageId,omitempty"`
	ToolName   string         `json:"toolName"`
	Input      map[string]any `json:"input"`
	Output     map[string]any `json:"output"`
	Status     string         `json:"status"`
	DurationMs *int           `json:"durationMs,omitempty"`
	StepNumber int            `json:"stepNumber"`
	CreatedAt  time.Time      `json:"createdAt"`
}

// AgentRunStepDTO represents a single LLM invocation step within a run,
// grouping the assistant message with any tool calls made during that step.
type AgentRunStepDTO struct {
	StepNumber int                    `json:"stepNumber"`
	Messages   []*AgentRunMessageDTO  `json:"messages"`
	ToolCalls  []*AgentRunToolCallDTO `json:"toolCalls"`
}

// --- ToDTO methods ---

// ToDTO converts an AgentDefinition entity to AgentDefinitionDTO
func (d *AgentDefinition) ToDTO() *AgentDefinitionDTO {
	return &AgentDefinitionDTO{
		ID:             d.ID,
		ProductID:      d.ProductID,
		ProjectID:      d.ProjectID,
		Name:           d.Name,
		Description:    d.Description,
		SystemPrompt:   d.SystemPrompt,
		Model:          d.Model,
		Tools:          d.Tools,
		FlowType:       d.FlowType,
		IsDefault:      d.IsDefault,
		MaxSteps:       d.MaxSteps,
		DefaultTimeout: d.DefaultTimeout,
		Visibility:     d.Visibility,
		DispatchMode:   d.DispatchMode,
		ACPConfig:      d.ACPConfig,
		Config:         d.Config,
		SandboxConfig:  d.SandboxConfig,
		CreatedAt:      d.CreatedAt,
		UpdatedAt:      d.UpdatedAt,
	}
}

// ToSummaryDTO converts an AgentDefinition entity to AgentDefinitionSummaryDTO
func (d *AgentDefinition) ToSummaryDTO() *AgentDefinitionSummaryDTO {
	return &AgentDefinitionSummaryDTO{
		ID:               d.ID,
		ProjectID:        d.ProjectID,
		Name:             d.Name,
		Description:      d.Description,
		FlowType:         d.FlowType,
		Visibility:       d.Visibility,
		IsDefault:        d.IsDefault,
		ToolCount:        len(d.Tools),
		HasSandboxConfig: len(d.SandboxConfig) > 0,
		CreatedAt:        d.CreatedAt,
		UpdatedAt:        d.UpdatedAt,
	}
}

// ToDTO converts an AgentRunMessage entity to AgentRunMessageDTO
func (m *AgentRunMessage) ToDTO() *AgentRunMessageDTO {
	return &AgentRunMessageDTO{
		ID:         m.ID,
		RunID:      m.RunID,
		Role:       m.Role,
		Content:    m.Content,
		StepNumber: m.StepNumber,
		CreatedAt:  m.CreatedAt,
	}
}

// ToDTO converts an AgentRunToolCall entity to AgentRunToolCallDTO
func (tc *AgentRunToolCall) ToDTO() *AgentRunToolCallDTO {
	return &AgentRunToolCallDTO{
		ID:         tc.ID,
		RunID:      tc.RunID,
		MessageID:  tc.MessageID,
		ToolName:   tc.ToolName,
		Input:      tc.Input,
		Output:     tc.Output,
		Status:     tc.Status,
		DurationMs: tc.DurationMs,
		StepNumber: tc.StepNumber,
		CreatedAt:  tc.CreatedAt,
	}
}

// --- Webhook Hooks DTOs ---

type AgentWebhookHookDTO struct {
	ID              string           `json:"id"`
	AgentID         string           `json:"agentId"`
	ProjectID       string           `json:"projectId"`
	Label           string           `json:"label"`
	Enabled         bool             `json:"enabled"`
	RateLimitConfig *RateLimitConfig `json:"rateLimitConfig"`
	CreatedAt       time.Time        `json:"createdAt"`
	UpdatedAt       time.Time        `json:"updatedAt"`
	Token           *string          `json:"token,omitempty"` // Only present on creation
}

func (h *AgentWebhookHook) ToDTO() *AgentWebhookHookDTO {
	return &AgentWebhookHookDTO{
		ID:              h.ID,
		AgentID:         h.AgentID,
		ProjectID:       h.ProjectID,
		Label:           h.Label,
		Enabled:         h.Enabled,
		RateLimitConfig: h.RateLimitConfig,
		CreatedAt:       h.CreatedAt,
		UpdatedAt:       h.UpdatedAt,
		Token:           h.Token,
	}
}

type CreateAgentWebhookHookDTO struct {
	Label           string           `json:"label" validate:"required"`
	RateLimitConfig *RateLimitConfig `json:"rateLimitConfig"`
}

type WebhookTriggerPayloadDTO struct {
	Prompt  string         `json:"prompt"`
	Context map[string]any `json:"context"`
}

// --- Agent Question DTOs ---

// AgentQuestionDTO is the response DTO for an agent question.
type AgentQuestionDTO struct {
	ID             string                `json:"id"`
	RunID          string                `json:"runId"`
	AgentID        string                `json:"agentId"`
	ProjectID      string                `json:"projectId"`
	Question       string                `json:"question"`
	Options        []AgentQuestionOption `json:"options"`
	Response       *string               `json:"response,omitempty"`
	RespondedBy    *string               `json:"respondedBy,omitempty"`
	RespondedAt    *time.Time            `json:"respondedAt,omitempty"`
	Status         AgentQuestionStatus   `json:"status"`
	NotificationID *string               `json:"notificationId,omitempty"`
	CreatedAt      time.Time             `json:"createdAt"`
	UpdatedAt      time.Time             `json:"updatedAt"`
}

// ToDTO converts an AgentQuestion entity to a DTO.
func (q *AgentQuestion) ToDTO() *AgentQuestionDTO {
	return &AgentQuestionDTO{
		ID:             q.ID,
		RunID:          q.RunID,
		AgentID:        q.AgentID,
		ProjectID:      q.ProjectID,
		Question:       q.Question,
		Options:        q.Options,
		Response:       q.Response,
		RespondedBy:    q.RespondedBy,
		RespondedAt:    q.RespondedAt,
		Status:         q.Status,
		NotificationID: q.NotificationID,
		CreatedAt:      q.CreatedAt,
		UpdatedAt:      q.UpdatedAt,
	}
}

// RespondToQuestionRequest is the request body for responding to an agent question.
type RespondToQuestionRequest struct {
	Response string `json:"response" validate:"required"`
}

// ADKEventDTO represents an event within an ADK session.
type ADKEventDTO struct {
	ID                     string         `json:"id"`
	SessionID              string         `json:"sessionId"`
	InvocationID           string         `json:"invocationId,omitempty"`
	Author                 string         `json:"author,omitempty"`
	Timestamp              time.Time      `json:"timestamp"`
	Branch                 *string        `json:"branch,omitempty"`
	Actions                map[string]any `json:"actions,omitempty"`
	LongRunningToolIDsJSON map[string]any `json:"longRunningToolIds,omitempty"`
	Content                map[string]any `json:"content,omitempty"`
	GroundingMetadata      map[string]any `json:"groundingMetadata,omitempty"`
	CustomMetadata         map[string]any `json:"customMetadata,omitempty"`
	UsageMetadata          map[string]any `json:"usageMetadata,omitempty"`
	CitationMetadata       map[string]any `json:"citationMetadata,omitempty"`
	Partial                *bool          `json:"partial,omitempty"`
	TurnComplete           *bool          `json:"turnComplete,omitempty"`
	ErrorCode              *string        `json:"errorCode,omitempty"`
	ErrorMessage           *string        `json:"errorMessage,omitempty"`
	Interrupted            *bool          `json:"interrupted,omitempty"`
}

// ADKSessionDTO represents an ADK session.
type ADKSessionDTO struct {
	ID         string         `json:"id"`
	AppName    string         `json:"appName"`
	UserID     string         `json:"userId"`
	State      map[string]any `json:"state,omitempty"`
	CreateTime time.Time      `json:"createTime"`
	UpdateTime time.Time      `json:"updateTime"`
	Events     []*ADKEventDTO `json:"events,omitempty"`
}
