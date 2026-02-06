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
