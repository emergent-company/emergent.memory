package main

import (
	"context"
	"net/http"
	"net/url"
)

// --- scheduled agents (memory runtime agents, kb.agents) ---

// ScheduledAgent mirrors memory's AgentDTO for runtime scheduled agents
// (table kb.agents). Field names are camelCase on the wire. Timestamps are
// RFC3339 strings. StrategyType has no server default — the gateway always
// uses "definition" (config resolved via agentDefinitionId).
type ScheduledAgent struct {
	ID                  string  `json:"id"`
	ProjectID           string  `json:"projectId"`
	Name                string  `json:"name"`
	StrategyType        string  `json:"strategyType"`
	Prompt              *string `json:"prompt"`
	CronSchedule        string  `json:"cronSchedule"`
	Enabled             bool    `json:"enabled"`
	TriggerType         string  `json:"triggerType"`
	Description         *string `json:"description"`
	LastRunAt           *string `json:"lastRunAt"`
	LastRunStatus       *string `json:"lastRunStatus"`
	ConsecutiveFailures int     `json:"consecutiveFailures"`
	AgentDefinitionID   *string `json:"agentDefinitionId,omitempty"`
	CreatedAt           string  `json:"createdAt"`
	UpdatedAt           string  `json:"updatedAt"`
}

// ScheduledAgentRun mirrors memory's AgentRunDTO (table kb.agent_runs) for one
// execution of a scheduled agent.
type ScheduledAgentRun struct {
	ID            string         `json:"id"`
	AgentID       string         `json:"agentId"`
	AgentName     string         `json:"agentName,omitempty"`
	Status        string         `json:"status"`
	StartedAt     string         `json:"startedAt"`
	CompletedAt   *string        `json:"completedAt"`
	Summary       map[string]any `json:"summary"`
	ErrorMessage  *string        `json:"errorMessage"`
	TriggerSource *string        `json:"triggerSource"`
}

// TriggerResult is the bare (unwrapped) response of POST /agents/:id/trigger:
// {success, runId?, message?, error?}.
type TriggerResult struct {
	Success bool    `json:"success"`
	RunID   *string `json:"runId,omitempty"`
	Message *string `json:"message,omitempty"`
	Error   *string `json:"error,omitempty"`
}

func (m *MemoryClient) scheduledAgentPath(ctx context.Context, id string) string {
	return "/api/projects/" + m.projectIDFor(ctx) + "/agents/" + url.PathEscape(id)
}

// ListScheduledAgents lists the project's runtime scheduled agents.
func (m *MemoryClient) ListScheduledAgents(ctx context.Context) ([]ScheduledAgent, error) {
	var env successEnvelope[[]ScheduledAgent]
	if err := m.do(ctx, http.MethodGet, "/api/projects/"+m.projectIDFor(ctx)+"/agents", nil, &env); err != nil {
		return nil, err
	}
	return env.Data, nil
}

// GetScheduledAgent fetches one runtime scheduled agent by id.
func (m *MemoryClient) GetScheduledAgent(ctx context.Context, id string) (*ScheduledAgent, error) {
	var env successEnvelope[ScheduledAgent]
	if err := m.do(ctx, http.MethodGet, m.scheduledAgentPath(ctx, id), nil, &env); err != nil {
		return nil, err
	}
	return &env.Data, nil
}

// CreateScheduledAgent creates a runtime scheduled agent. ProjectID is forced
// to the client's project; strategyType must be set by the caller (memory has
// no server default).
func (m *MemoryClient) CreateScheduledAgent(ctx context.Context, in *ScheduledAgent) (*ScheduledAgent, error) {
	in.ProjectID = m.projectIDFor(ctx)
	var env successEnvelope[ScheduledAgent]
	if err := m.do(ctx, http.MethodPost, "/api/projects/"+m.projectIDFor(ctx)+"/agents", in, &env); err != nil {
		return nil, err
	}
	return &env.Data, nil
}

// UpdateScheduledAgent partially updates a runtime scheduled agent.
func (m *MemoryClient) UpdateScheduledAgent(ctx context.Context, id string, in *ScheduledAgent) (*ScheduledAgent, error) {
	var env successEnvelope[ScheduledAgent]
	if err := m.do(ctx, http.MethodPatch, m.scheduledAgentPath(ctx, id), in, &env); err != nil {
		return nil, err
	}
	return &env.Data, nil
}

// EnableScheduledAgent re-enables a disabled scheduled agent.
func (m *MemoryClient) EnableScheduledAgent(ctx context.Context, id string) (*ScheduledAgent, error) {
	var env successEnvelope[ScheduledAgent]
	if err := m.do(ctx, http.MethodPost, m.scheduledAgentPath(ctx, id)+"/enable", nil, &env); err != nil {
		return nil, err
	}
	return &env.Data, nil
}

// SetScheduledAgentEnabled enables or disables a scheduled agent via a partial
// PATCH — memory's UpdateAgent applies only the provided (non-nil) fields.
func (m *MemoryClient) SetScheduledAgentEnabled(ctx context.Context, id string, enabled bool) (*ScheduledAgent, error) {
	var env successEnvelope[ScheduledAgent]
	if err := m.do(ctx, http.MethodPatch, m.scheduledAgentPath(ctx, id), map[string]any{"enabled": enabled}, &env); err != nil {
		return nil, err
	}
	return &env.Data, nil
}

// DeleteScheduledAgent removes a scheduled agent. The response body is
// ignored; only the transport error matters.
func (m *MemoryClient) DeleteScheduledAgent(ctx context.Context, id string) error {
	return m.do(ctx, http.MethodDelete, m.scheduledAgentPath(ctx, id), nil, nil)
}

// TriggerScheduledAgent fires a scheduled agent immediately. The trigger
// response is a bare TriggerResult (NOT wrapped in the {success,data,error}
// envelope), so it is parsed directly.
func (m *MemoryClient) TriggerScheduledAgent(ctx context.Context, id string) (*TriggerResult, error) {
	var out TriggerResult
	if err := m.do(ctx, http.MethodPost, m.scheduledAgentPath(ctx, id)+"/trigger", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// triggerAgentRequest is the JSON body for POST /agents/:id/trigger. Unlike
// the scheduled auto-trigger (which sends no body), a webhook-triggered review
// carries a prompt and a context map.
type triggerAgentRequest struct {
	Prompt  string            `json:"prompt"`
	Context map[string]string `json:"context,omitempty"`
}

// TriggerAgent fires a runtime agent with an explicit prompt and context
// (e.g. a GitHub pull_request webhook; see webhook_github.go). The response is
// the same bare TriggerResult shape as TriggerScheduledAgent.
func (m *MemoryClient) TriggerAgent(ctx context.Context, agentID, prompt string, ctxValues map[string]string) (*TriggerResult, error) {
	var out TriggerResult
	body := triggerAgentRequest{Prompt: prompt, Context: ctxValues}
	if err := m.do(ctx, http.MethodPost, m.scheduledAgentPath(ctx, agentID)+"/trigger", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListScheduledAgentRuns lists the execution history of one scheduled agent.
func (m *MemoryClient) ListScheduledAgentRuns(ctx context.Context, id string) ([]ScheduledAgentRun, error) {
	var env successEnvelope[[]ScheduledAgentRun]
	if err := m.do(ctx, http.MethodGet, m.scheduledAgentPath(ctx, id)+"/runs", nil, &env); err != nil {
		return nil, err
	}
	return env.Data, nil
}
