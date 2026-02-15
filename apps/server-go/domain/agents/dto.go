package agents

import "time"

// AgentDTO is the response DTO for an agent
type AgentDTO struct {
	ID             string             `json:"id"`
	ProjectID      string             `json:"projectId"`
	Name           string             `json:"name"`
	StrategyType   string             `json:"strategyType"`
	Prompt         *string            `json:"prompt"`
	CronSchedule   string             `json:"cronSchedule"`
	Enabled        bool               `json:"enabled"`
	TriggerType    AgentTriggerType   `json:"triggerType"`
	ReactionConfig *ReactionConfig    `json:"reactionConfig"`
	ExecutionMode  AgentExecutionMode `json:"executionMode"`
	Capabilities   *AgentCapabilities `json:"capabilities"`
	Config         map[string]any     `json:"config"`
	Description    *string            `json:"description"`
	LastRunAt      *time.Time         `json:"lastRunAt"`
	LastRunStatus  *string            `json:"lastRunStatus"`
	CreatedAt      time.Time          `json:"createdAt"`
	UpdatedAt      time.Time          `json:"updatedAt"`
}

// AgentRunDTO is the response DTO for an agent run
type AgentRunDTO struct {
	ID           string         `json:"id"`
	AgentID      string         `json:"agentId"`
	Status       AgentRunStatus `json:"status"`
	StartedAt    time.Time      `json:"startedAt"`
	CompletedAt  *time.Time     `json:"completedAt"`
	DurationMs   *int           `json:"durationMs"`
	Summary      map[string]any `json:"summary"`
	ErrorMessage *string        `json:"errorMessage"`
	SkipReason   *string        `json:"skipReason"`

	// Multi-agent coordination fields
	ParentRunID *string `json:"parentRunId,omitempty"`
	StepCount   int     `json:"stepCount"`
	MaxSteps    *int    `json:"maxSteps,omitempty"`
	ResumedFrom *string `json:"resumedFrom,omitempty"`
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
		ID:             a.ID,
		ProjectID:      a.ProjectID,
		Name:           a.Name,
		StrategyType:   a.StrategyType,
		Prompt:         a.Prompt,
		CronSchedule:   a.CronSchedule,
		Enabled:        a.Enabled,
		TriggerType:    a.TriggerType,
		ReactionConfig: a.ReactionConfig,
		ExecutionMode:  a.ExecutionMode,
		Capabilities:   a.Capabilities,
		Config:         a.Config,
		Description:    a.Description,
		LastRunAt:      a.LastRunAt,
		LastRunStatus:  a.LastRunStatus,
		CreatedAt:      a.CreatedAt,
		UpdatedAt:      a.UpdatedAt,
	}
}

// ToDTO converts an AgentRun entity to AgentRunDTO
func (r *AgentRun) ToDTO() *AgentRunDTO {
	return &AgentRunDTO{
		ID:           r.ID,
		AgentID:      r.AgentID,
		Status:       r.Status,
		StartedAt:    r.StartedAt,
		CompletedAt:  r.CompletedAt,
		DurationMs:   r.DurationMs,
		Summary:      r.Summary,
		ErrorMessage: r.ErrorMessage,
		SkipReason:   r.SkipReason,
		ParentRunID:  r.ParentRunID,
		StepCount:    r.StepCount,
		MaxSteps:     r.MaxSteps,
		ResumedFrom:  r.ResumedFrom,
	}
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

// --- Agent Definition DTOs ---

// AgentDefinitionDTO is the full response DTO for an agent definition
type AgentDefinitionDTO struct {
	ID             string          `json:"id"`
	ProductID      *string         `json:"productId,omitempty"`
	ProjectID      string          `json:"projectId"`
	Name           string          `json:"name"`
	Description    *string         `json:"description,omitempty"`
	SystemPrompt   *string         `json:"systemPrompt,omitempty"`
	Model          *ModelConfig    `json:"model,omitempty"`
	Tools          []string        `json:"tools"`
	Trigger        *string         `json:"trigger,omitempty"`
	FlowType       AgentFlowType   `json:"flowType"`
	IsDefault      bool            `json:"isDefault"`
	MaxSteps       *int            `json:"maxSteps,omitempty"`
	DefaultTimeout *int            `json:"defaultTimeout,omitempty"`
	Visibility     AgentVisibility `json:"visibility"`
	ACPConfig      *ACPConfig      `json:"acpConfig,omitempty"`
	Config         map[string]any  `json:"config,omitempty"`
	CreatedAt      time.Time       `json:"createdAt"`
	UpdatedAt      time.Time       `json:"updatedAt"`
}

// AgentDefinitionSummaryDTO is a lightweight DTO for listing agent definitions
type AgentDefinitionSummaryDTO struct {
	ID          string          `json:"id"`
	ProjectID   string          `json:"projectId"`
	Name        string          `json:"name"`
	Description *string         `json:"description,omitempty"`
	FlowType    AgentFlowType   `json:"flowType"`
	Visibility  AgentVisibility `json:"visibility"`
	IsDefault   bool            `json:"isDefault"`
	ToolCount   int             `json:"toolCount"`
	CreatedAt   time.Time       `json:"createdAt"`
	UpdatedAt   time.Time       `json:"updatedAt"`
}

// CreateAgentDefinitionDTO is the request DTO for creating an agent definition
type CreateAgentDefinitionDTO struct {
	Name           string          `json:"name" validate:"required"`
	Description    *string         `json:"description"`
	SystemPrompt   *string         `json:"systemPrompt"`
	Model          *ModelConfig    `json:"model"`
	Tools          []string        `json:"tools"`
	Trigger        *string         `json:"trigger"`
	FlowType       AgentFlowType   `json:"flowType"`
	IsDefault      *bool           `json:"isDefault"`
	MaxSteps       *int            `json:"maxSteps"`
	DefaultTimeout *int            `json:"defaultTimeout"`
	Visibility     AgentVisibility `json:"visibility"`
	ACPConfig      *ACPConfig      `json:"acpConfig"`
	Config         map[string]any  `json:"config"`
}

// UpdateAgentDefinitionDTO is the request DTO for updating an agent definition
type UpdateAgentDefinitionDTO struct {
	Name           *string          `json:"name"`
	Description    *string          `json:"description"`
	SystemPrompt   *string          `json:"systemPrompt"`
	Model          *ModelConfig     `json:"model"`
	Tools          []string         `json:"tools"`
	Trigger        *string          `json:"trigger"`
	FlowType       *AgentFlowType   `json:"flowType"`
	IsDefault      *bool            `json:"isDefault"`
	MaxSteps       *int             `json:"maxSteps"`
	DefaultTimeout *int             `json:"defaultTimeout"`
	Visibility     *AgentVisibility `json:"visibility"`
	ACPConfig      *ACPConfig       `json:"acpConfig"`
	Config         map[string]any   `json:"config"`
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
		Trigger:        d.Trigger,
		FlowType:       d.FlowType,
		IsDefault:      d.IsDefault,
		MaxSteps:       d.MaxSteps,
		DefaultTimeout: d.DefaultTimeout,
		Visibility:     d.Visibility,
		ACPConfig:      d.ACPConfig,
		Config:         d.Config,
		CreatedAt:      d.CreatedAt,
		UpdatedAt:      d.UpdatedAt,
	}
}

// ToSummaryDTO converts an AgentDefinition entity to AgentDefinitionSummaryDTO
func (d *AgentDefinition) ToSummaryDTO() *AgentDefinitionSummaryDTO {
	return &AgentDefinitionSummaryDTO{
		ID:          d.ID,
		ProjectID:   d.ProjectID,
		Name:        d.Name,
		Description: d.Description,
		FlowType:    d.FlowType,
		Visibility:  d.Visibility,
		IsDefault:   d.IsDefault,
		ToolCount:   len(d.Tools),
		CreatedAt:   d.CreatedAt,
		UpdatedAt:   d.UpdatedAt,
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
