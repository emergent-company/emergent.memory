package agents

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/auth"
	sdkerrors "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/errors"
)

// Client provides access to the Agents API.
type Client struct {
	http      *http.Client
	base      string
	auth      auth.Provider
	mu        sync.RWMutex
	orgID     string
	projectID string
}

// NewClient creates a new agents client.
func NewClient(httpClient *http.Client, baseURL string, authProvider auth.Provider, orgID, projectID string) *Client {
	return &Client{
		http:      httpClient,
		base:      baseURL,
		auth:      authProvider,
		orgID:     orgID,
		projectID: projectID,
	}
}

// SetContext sets the organization and project context.
func (c *Client) SetContext(orgID, projectID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.orgID = orgID
	c.projectID = projectID
}

// --- Types ---

// Agent represents an agent entity.
type Agent struct {
	ID             string             `json:"id"`
	ProjectID      string             `json:"projectId"`
	Name           string             `json:"name"`
	StrategyType   string             `json:"strategyType"`
	Prompt         *string            `json:"prompt"`
	CronSchedule   string             `json:"cronSchedule"`
	Enabled        bool               `json:"enabled"`
	TriggerType    string             `json:"triggerType"`
	ReactionConfig *ReactionConfig    `json:"reactionConfig"`
	ExecutionMode  string             `json:"executionMode"`
	Capabilities   *AgentCapabilities `json:"capabilities"`
	Config         map[string]any     `json:"config"`
	Description    *string            `json:"description"`
	LastRunAt      *time.Time         `json:"lastRunAt"`
	LastRunStatus  *string            `json:"lastRunStatus"`
	CreatedAt      time.Time          `json:"createdAt"`
	UpdatedAt      time.Time          `json:"updatedAt"`
}

// ReactionConfig contains configuration for reaction triggers.
type ReactionConfig struct {
	ObjectTypes          []string `json:"objectTypes"`
	Events               []string `json:"events"`
	ConcurrencyStrategy  string   `json:"concurrencyStrategy"`
	IgnoreAgentTriggered bool     `json:"ignoreAgentTriggered"`
	IgnoreSelfTriggered  bool     `json:"ignoreSelfTriggered"`
}

// AgentCapabilities defines capability restrictions for agents.
type AgentCapabilities struct {
	CanCreateObjects       *bool    `json:"canCreateObjects,omitempty"`
	CanUpdateObjects       *bool    `json:"canUpdateObjects,omitempty"`
	CanDeleteObjects       *bool    `json:"canDeleteObjects,omitempty"`
	CanCreateRelationships *bool    `json:"canCreateRelationships,omitempty"`
	AllowedObjectTypes     []string `json:"allowedObjectTypes,omitempty"`
}

// RunTokenUsage holds aggregated LLM token counts and estimated cost for a run.
type RunTokenUsage struct {
	TotalInputTokens  int64   `json:"totalInputTokens"`
	TotalOutputTokens int64   `json:"totalOutputTokens"`
	EstimatedCostUSD  float64 `json:"estimatedCostUsd"`
}

// AgentRunWorkspace holds sandbox container info for a run.
type AgentRunWorkspace struct {
	Provider    string `json:"provider"`
	ContainerID string `json:"containerId"`
	BaseImage   string `json:"baseImage"`
	Digest      string `json:"digest"`
}

// AgentRun represents an agent run record.
type AgentRun struct {
	ID           string         `json:"id"`
	AgentID      string         `json:"agentId"`
	AgentName    string         `json:"agentName,omitempty"`
	Status       string         `json:"status"`
	StartedAt    time.Time      `json:"startedAt"`
	CompletedAt  *time.Time     `json:"completedAt"`
	DurationMs   *int           `json:"durationMs"`
	Summary      map[string]any `json:"summary"`
	ErrorMessage *string        `json:"errorMessage"`
	SkipReason   *string        `json:"skipReason"`

	// Model and provider used for this run
	Model    *string `json:"model,omitempty"`
	Provider *string `json:"provider,omitempty"`

	// Workspace info (sandbox container)
	Workspace *AgentRunWorkspace `json:"workspace,omitempty"`

	// Execution metrics
	StepCount int  `json:"stepCount"`
	MaxSteps  *int `json:"maxSteps,omitempty"`

	// Multi-agent coordination
	ParentRunID *string `json:"parentRunId,omitempty"`
	ResumedFrom *string `json:"resumedFrom,omitempty"`

	// Observability linkage
	TraceID   *string `json:"traceId,omitempty"`
	RootRunID *string `json:"rootRunId,omitempty"`

	// Trigger tracking
	TriggerSource   *string        `json:"triggerSource,omitempty"`
	TriggerMetadata map[string]any `json:"triggerMetadata,omitempty"`

	// Token usage and cost (populated by GetProjectRun, nil for list endpoints)
	TokenUsage *RunTokenUsage `json:"tokenUsage,omitempty"`
}

// AgentRunMessage represents a message in the agent conversation.
type AgentRunMessage struct {
	ID         string         `json:"id"`
	RunID      string         `json:"runId"`
	Role       string         `json:"role"`
	Content    map[string]any `json:"content"`
	StepNumber int            `json:"stepNumber"`
	CreatedAt  time.Time      `json:"createdAt"`
}

// AgentRunToolCall represents a tool invocation record.
type AgentRunToolCall struct {
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

// AgentQuestion represents a question posed by an agent to a user during execution.
type AgentQuestion struct {
	ID             string                `json:"id"`
	RunID          string                `json:"runId"`
	AgentID        string                `json:"agentId"`
	ProjectID      string                `json:"projectId"`
	Question       string                `json:"question"`
	Options        []AgentQuestionOption `json:"options"`
	Response       *string               `json:"response,omitempty"`
	RespondedBy    *string               `json:"respondedBy,omitempty"`
	RespondedAt    *time.Time            `json:"respondedAt,omitempty"`
	Status         string                `json:"status"`
	NotificationID *string               `json:"notificationId,omitempty"`
	CreatedAt      time.Time             `json:"createdAt"`
	UpdatedAt      time.Time             `json:"updatedAt"`
}

// AgentQuestionOption represents a structured choice for an agent question.
type AgentQuestionOption struct {
	Label       string `json:"label"`
	Value       string `json:"value"`
	Description string `json:"description,omitempty"`
}

// RespondToQuestionRequest is the request body for responding to an agent question.
type RespondToQuestionRequest struct {
	Response string `json:"response"`
}

// PaginatedResponse wraps paginated API responses.
type PaginatedResponse[T any] struct {
	Items      []T `json:"items"`
	TotalCount int `json:"totalCount"`
	Limit      int `json:"limit"`
	Offset     int `json:"offset"`
}

// ListRunsOptions contains options for listing project runs.
type ListRunsOptions struct {
	Limit   int
	Offset  int
	AgentID string
	Status  string
}

// APIResponse wraps API responses with success flag.
type APIResponse[T any] struct {
	Success bool    `json:"success"`
	Data    T       `json:"data,omitempty"`
	Error   *string `json:"error,omitempty"`
	Message *string `json:"message,omitempty"`
}

// CreateAgentRequest is the request body for creating an agent.
type CreateAgentRequest struct {
	ProjectID         string             `json:"projectId"`
	Name              string             `json:"name"`
	StrategyType      string             `json:"strategyType"`
	Prompt            *string            `json:"prompt,omitempty"`
	CronSchedule      string             `json:"cronSchedule"`
	Enabled           *bool              `json:"enabled,omitempty"`
	TriggerType       string             `json:"triggerType,omitempty"`
	ReactionConfig    *ReactionConfig    `json:"reactionConfig,omitempty"`
	ExecutionMode     string             `json:"executionMode,omitempty"`
	Capabilities      *AgentCapabilities `json:"capabilities,omitempty"`
	Config            map[string]any     `json:"config,omitempty"`
	Description       *string            `json:"description,omitempty"`
	AgentDefinitionID *string            `json:"agentDefinitionId,omitempty"`
}

// UpdateAgentRequest is the request body for updating an agent.
type UpdateAgentRequest struct {
	Name           *string            `json:"name,omitempty"`
	Prompt         *string            `json:"prompt,omitempty"`
	Enabled        *bool              `json:"enabled,omitempty"`
	CronSchedule   *string            `json:"cronSchedule,omitempty"`
	TriggerType    *string            `json:"triggerType,omitempty"`
	ReactionConfig *ReactionConfig    `json:"reactionConfig,omitempty"`
	ExecutionMode  *string            `json:"executionMode,omitempty"`
	Capabilities   *AgentCapabilities `json:"capabilities,omitempty"`
	Config         map[string]any     `json:"config,omitempty"`
	Description    *string            `json:"description,omitempty"`
}

// BatchTriggerRequest is the request body for batch triggering an agent.
type BatchTriggerRequest struct {
	ObjectIDs []string `json:"objectIds"`
}

// BatchTriggerResponse is the response for batch trigger.
type BatchTriggerResponse struct {
	Queued         int `json:"queued"`
	Skipped        int `json:"skipped"`
	SkippedDetails []struct {
		ObjectID string `json:"objectId"`
		Reason   string `json:"reason"`
	} `json:"skippedDetails"`
}

// TriggerResponse is the response for triggering an agent.
type TriggerResponse struct {
	Success bool    `json:"success"`
	RunID   *string `json:"runId,omitempty"`
	Message *string `json:"message,omitempty"`
	Error   *string `json:"error,omitempty"`
}

// TriggerRequest is the optional request body for triggering an agent with input.
type TriggerRequest struct {
	// Input is an initial user message passed to the agent at trigger time.
	Input string `json:"prompt,omitempty"`
	// Metadata is arbitrary caller context injected into the agent's system instruction.
	Metadata map[string]string `json:"context,omitempty"`
	// Model overrides the agent definition's model for this single run.
	Model string `json:"model,omitempty"`
	// EnvVars are runtime environment variables injected into the sandbox container.
	EnvVars map[string]string `json:"env_vars,omitempty"`
}

// PendingEventObject represents a graph object pending processing.
type PendingEventObject struct {
	ID        string    `json:"id"`
	Type      string    `json:"type"`
	Key       string    `json:"key"`
	Version   int       `json:"version"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// PendingEventsResponse is the response for pending events query.
type PendingEventsResponse struct {
	TotalCount     int                  `json:"totalCount"`
	Objects        []PendingEventObject `json:"objects"`
	ReactionConfig struct {
		ObjectTypes []string `json:"objectTypes"`
		Events      []string `json:"events"`
	} `json:"reactionConfig"`
}

// WebhookHook represents a webhook hook for triggering an agent.
type WebhookHook struct {
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

// RateLimitConfig configures rate limiting for a webhook hook.
type RateLimitConfig struct {
	RequestsPerMinute int `json:"requestsPerMinute"`
	BurstSize         int `json:"burstSize"`
}

// CreateWebhookHookRequest is the request body for creating a webhook hook.
type CreateWebhookHookRequest struct {
	Label           string           `json:"label"`
	RateLimitConfig *RateLimitConfig `json:"rateLimitConfig,omitempty"`
}

// --- Internal helpers ---

func (c *Client) setHeaders(req *http.Request) error {
	if err := c.auth.Authenticate(req); err != nil {
		return fmt.Errorf("authentication failed: %w", err)
	}
	c.mu.RLock()
	orgID := c.orgID
	projectID := c.projectID
	c.mu.RUnlock()
	if orgID != "" {
		req.Header.Set("X-Org-ID", orgID)
	}
	if projectID != "" {
		req.Header.Set("X-Project-ID", projectID)
	}
	return nil
}

// --- API Methods ---

// List returns all agents for the current project.
// GET /api/projects/:projectId/agents
// Requires project:read scope.
func (c *Client) List(ctx context.Context) (*APIResponse[[]Agent], error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.base+"/api/projects/"+url.PathEscape(c.projectID)+"/agents", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if err := c.setHeaders(req); err != nil {
		return nil, err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, sdkerrors.ParseErrorResponse(resp)
	}

	var result APIResponse[[]Agent]
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// Get retrieves a single agent by ID.
// GET /api/projects/:projectId/agents/:id
// Requires project:read scope.
func (c *Client) Get(ctx context.Context, agentID string) (*APIResponse[Agent], error) {
	req, err := http.NewRequestWithContext(
		ctx,
		"GET",
		c.base+"/api/projects/"+url.PathEscape(c.projectID)+"/agents/"+url.PathEscape(agentID),
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if err := c.setHeaders(req); err != nil {
		return nil, err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, sdkerrors.ParseErrorResponse(resp)
	}

	var result APIResponse[Agent]
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// GetRunQuestions gets questions for a run.
// GET /api/projects/:projectId/agent-runs/:runId/questions
// Requires project:read scope.
func (c *Client) GetRunQuestions(ctx context.Context, projectID, runID string) (*APIResponse[[]AgentQuestion], error) {
	req, err := http.NewRequestWithContext(
		ctx,
		"GET",
		c.base+"/api/projects/"+url.PathEscape(projectID)+"/agent-runs/"+url.PathEscape(runID)+"/questions",
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if err := c.setHeaders(req); err != nil {
		return nil, err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, sdkerrors.ParseErrorResponse(resp)
	}

	var result APIResponse[[]AgentQuestion]
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// ListProjectQuestions lists agent questions for a project with optional status filter.
// GET /api/projects/:projectId/agent-questions
// Requires project:read scope.
func (c *Client) ListProjectQuestions(ctx context.Context, projectID, status string) (*APIResponse[[]AgentQuestion], error) {
	u, err := url.Parse(c.base + "/api/projects/" + url.PathEscape(projectID) + "/agent-questions")
	if err != nil {
		return nil, fmt.Errorf("failed to parse URL: %w", err)
	}

	if status != "" {
		q := u.Query()
		q.Set("status", status)
		u.RawQuery = q.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if err := c.setHeaders(req); err != nil {
		return nil, err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, sdkerrors.ParseErrorResponse(resp)
	}

	var result APIResponse[[]AgentQuestion]
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// RespondToQuestion responds to a pending agent question and resumes the paused run.
// POST /api/projects/:projectId/agent-questions/:questionId/respond
// Returns 202 Accepted on success.
func (c *Client) RespondToQuestion(ctx context.Context, projectID, questionID string, respondReq *RespondToQuestionRequest) (*APIResponse[AgentQuestion], error) {
	body, err := json.Marshal(respondReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(
		ctx,
		"POST",
		c.base+"/api/projects/"+url.PathEscape(projectID)+"/agent-questions/"+url.PathEscape(questionID)+"/respond",
		bytes.NewReader(body),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if err := c.setHeaders(req); err != nil {
		return nil, err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, sdkerrors.ParseErrorResponse(resp)
	}

	var result APIResponse[AgentQuestion]
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// GetRuns returns recent runs for an agent.
// GET /api/projects/:projectId/agents/:id/runs
// Requires project:read scope.
func (c *Client) GetRuns(ctx context.Context, agentID string, limit int) (*APIResponse[[]AgentRun], error) {
	u, err := url.Parse(c.base + "/api/projects/" + url.PathEscape(c.projectID) + "/agents/" + url.PathEscape(agentID) + "/runs")
	if err != nil {
		return nil, fmt.Errorf("failed to parse URL: %w", err)
	}

	if limit > 0 {
		q := u.Query()
		q.Set("limit", fmt.Sprintf("%d", limit))
		u.RawQuery = q.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if err := c.setHeaders(req); err != nil {
		return nil, err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, sdkerrors.ParseErrorResponse(resp)
	}

	var result APIResponse[[]AgentRun]
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// GetPendingEvents returns pending events for a reaction agent.
// GET /api/projects/:projectId/agents/:id/pending-events
// Requires project:read scope.
func (c *Client) GetPendingEvents(ctx context.Context, agentID string, limit int) (*APIResponse[PendingEventsResponse], error) {
	u, err := url.Parse(c.base + "/api/projects/" + url.PathEscape(c.projectID) + "/agents/" + url.PathEscape(agentID) + "/pending-events")
	if err != nil {
		return nil, fmt.Errorf("failed to parse URL: %w", err)
	}

	if limit > 0 {
		q := u.Query()
		q.Set("limit", fmt.Sprintf("%d", limit))
		u.RawQuery = q.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if err := c.setHeaders(req); err != nil {
		return nil, err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, sdkerrors.ParseErrorResponse(resp)
	}

	var result APIResponse[PendingEventsResponse]
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// Create creates a new agent.
// POST /api/projects/:projectId/agents
// Requires project:write scope. Returns 201 on success.
func (c *Client) Create(ctx context.Context, createReq *CreateAgentRequest) (*APIResponse[Agent], error) {
	body, err := json.Marshal(createReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.base+"/api/projects/"+url.PathEscape(c.projectID)+"/agents", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if err := c.setHeaders(req); err != nil {
		return nil, err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, sdkerrors.ParseErrorResponse(resp)
	}

	var result APIResponse[Agent]
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// Update updates an agent (partial update via PATCH).
// PATCH /api/projects/:projectId/agents/:id
// Requires project:write scope.
func (c *Client) Update(ctx context.Context, agentID string, updateReq *UpdateAgentRequest) (*APIResponse[Agent], error) {
	body, err := json.Marshal(updateReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "PATCH", c.base+"/api/projects/"+url.PathEscape(c.projectID)+"/agents/"+url.PathEscape(agentID), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if err := c.setHeaders(req); err != nil {
		return nil, err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, sdkerrors.ParseErrorResponse(resp)
	}

	var result APIResponse[Agent]
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// Delete deletes an agent by ID.
// DELETE /api/projects/:projectId/agents/:id
// Requires project:write scope.
func (c *Client) Delete(ctx context.Context, agentID string) error {
	req, err := http.NewRequestWithContext(ctx, "DELETE", c.base+"/api/projects/"+url.PathEscape(c.projectID)+"/agents/"+url.PathEscape(agentID), nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	if err := c.setHeaders(req); err != nil {
		return err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return sdkerrors.ParseErrorResponse(resp)
	}

	_, _ = io.Copy(io.Discard, resp.Body)

	return nil
}

// Trigger triggers an immediate run of an agent.
// POST /api/projects/:projectId/agents/:id/trigger
// Requires project:write scope.
func (c *Client) Trigger(ctx context.Context, agentID string) (*TriggerResponse, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", c.base+"/api/projects/"+url.PathEscape(c.projectID)+"/agents/"+url.PathEscape(agentID)+"/trigger", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if err := c.setHeaders(req); err != nil {
		return nil, err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, sdkerrors.ParseErrorResponse(resp)
	}

	var result TriggerResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// TriggerWithInput triggers an immediate run of an agent with an optional input message.
// POST /api/projects/:projectId/agents/:id/trigger
// Requires project:write scope.
func (c *Client) TriggerWithInput(ctx context.Context, agentID string, req TriggerRequest) (*TriggerResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.base+"/api/projects/"+url.PathEscape(c.projectID)+"/agents/"+url.PathEscape(agentID)+"/trigger", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	if err := c.setHeaders(httpReq); err != nil {
		return nil, err
	}

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, sdkerrors.ParseErrorResponse(resp)
	}

	var result TriggerResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// BatchTrigger triggers a reaction agent for multiple graph objects.
// POST /api/projects/:projectId/agents/:id/batch-trigger
// Requires project:write scope. Max 100 objects per batch.
func (c *Client) BatchTrigger(ctx context.Context, agentID string, batchReq *BatchTriggerRequest) (*APIResponse[BatchTriggerResponse], error) {
	body, err := json.Marshal(batchReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.base+"/api/projects/"+url.PathEscape(c.projectID)+"/agents/"+url.PathEscape(agentID)+"/batch-trigger", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if err := c.setHeaders(req); err != nil {
		return nil, err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, sdkerrors.ParseErrorResponse(resp)
	}

	var result APIResponse[BatchTriggerResponse]
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// CancelRun cancels a running agent run.
// POST /api/projects/:projectId/agents/:id/runs/:runId/cancel
// Requires project:write scope.
func (c *Client) CancelRun(ctx context.Context, agentID, runID string) error {
	req, err := http.NewRequestWithContext(
		ctx,
		"POST",
		c.base+"/api/projects/"+url.PathEscape(c.projectID)+"/agents/"+url.PathEscape(agentID)+"/runs/"+url.PathEscape(runID)+"/cancel",
		nil,
	)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	if err := c.setHeaders(req); err != nil {
		return err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return sdkerrors.ParseErrorResponse(resp)
	}

	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}

// ListProjectRuns lists agent runs for a project with filtering and pagination.
// GET /api/projects/:projectId/agent-runs
// Requires project:read scope.
func (c *Client) ListProjectRuns(ctx context.Context, projectID string, opts *ListRunsOptions) (*APIResponse[PaginatedResponse[AgentRun]], error) {
	u, err := url.Parse(c.base + "/api/projects/" + url.PathEscape(projectID) + "/agent-runs")
	if err != nil {
		return nil, fmt.Errorf("failed to parse URL: %w", err)
	}

	if opts != nil {
		q := u.Query()
		if opts.Limit > 0 {
			q.Set("limit", fmt.Sprintf("%d", opts.Limit))
		}
		if opts.Offset > 0 {
			q.Set("offset", fmt.Sprintf("%d", opts.Offset))
		}
		if opts.AgentID != "" {
			q.Set("agentId", opts.AgentID)
		}
		if opts.Status != "" {
			q.Set("status", opts.Status)
		}
		u.RawQuery = q.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if err := c.setHeaders(req); err != nil {
		return nil, err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, sdkerrors.ParseErrorResponse(resp)
	}

	var result APIResponse[PaginatedResponse[AgentRun]]
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// GetProjectRun gets a specific run by ID.
// GET /api/projects/:projectId/agent-runs/:runId
// Requires project:read scope.
func (c *Client) GetProjectRun(ctx context.Context, projectID, runID string) (*APIResponse[AgentRun], error) {
	req, err := http.NewRequestWithContext(
		ctx,
		"GET",
		c.base+"/api/projects/"+url.PathEscape(projectID)+"/agent-runs/"+url.PathEscape(runID),
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if err := c.setHeaders(req); err != nil {
		return nil, err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, sdkerrors.ParseErrorResponse(resp)
	}

	var result APIResponse[AgentRun]
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// GetRunByID gets a specific run by its globally unique ID — no project required.
// GET /api/v1/runs/:runId
// Requires agents:read scope.
func (c *Client) GetRunByID(ctx context.Context, runID string) (*APIResponse[AgentRun], error) {
	req, err := http.NewRequestWithContext(
		ctx,
		"GET",
		c.base+"/api/v1/runs/"+url.PathEscape(runID),
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if err := c.setHeaders(req); err != nil {
		return nil, err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, sdkerrors.ParseErrorResponse(resp)
	}

	var result APIResponse[AgentRun]
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// GetRunMessages gets conversation messages for a run.
// GET /api/projects/:projectId/agent-runs/:runId/messages
// Requires project:read scope.
func (c *Client) GetRunMessages(ctx context.Context, projectID, runID string) (*APIResponse[[]AgentRunMessage], error) {
	req, err := http.NewRequestWithContext(
		ctx,
		"GET",
		c.base+"/api/projects/"+url.PathEscape(projectID)+"/agent-runs/"+url.PathEscape(runID)+"/messages",
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if err := c.setHeaders(req); err != nil {
		return nil, err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, sdkerrors.ParseErrorResponse(resp)
	}

	var result APIResponse[[]AgentRunMessage]
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// GetRunToolCalls gets tool invocations for a run.
// GET /api/projects/:projectId/agent-runs/:runId/tool-calls
// Requires project:read scope.
func (c *Client) GetRunToolCalls(ctx context.Context, projectID, runID string) (*APIResponse[[]AgentRunToolCall], error) {
	req, err := http.NewRequestWithContext(
		ctx,
		"GET",
		c.base+"/api/projects/"+url.PathEscape(projectID)+"/agent-runs/"+url.PathEscape(runID)+"/tool-calls",
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if err := c.setHeaders(req); err != nil {
		return nil, err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, sdkerrors.ParseErrorResponse(resp)
	}

	var result APIResponse[[]AgentRunToolCall]
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// RunLogEntry is a unified log entry from GET /api/v1/runs/:runId/logs.
// Kind is either "message" or "tool_call".
type RunLogEntry struct {
	Kind       string    `json:"kind"`
	StepNumber int       `json:"stepNumber"`
	CreatedAt  time.Time `json:"createdAt"`
	// Message fields (Kind == "message")
	Role    string         `json:"role,omitempty"`
	Content map[string]any `json:"content,omitempty"`
	// Tool call fields (Kind == "tool_call")
	ToolName   string         `json:"toolName,omitempty"`
	Input      map[string]any `json:"input,omitempty"`
	Output     map[string]any `json:"output,omitempty"`
	Status     string         `json:"status,omitempty"`
	DurationMs *int           `json:"durationMs,omitempty"`
}

// GetRunLogs opens the SSE log stream for a run and returns the raw response
// body as an io.ReadCloser. The caller is responsible for closing it.
//
// The stream emits SSE events with event names "message", "tool_call", or "done".
// Each data field is a JSON-encoded RunLogEntry (or a done summary object).
//
// GET /api/v1/runs/:runId/logs
// Requires agents:read scope.
func (c *Client) GetRunLogs(ctx context.Context, runID string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(
		ctx,
		"GET",
		c.base+"/api/v1/runs/"+url.PathEscape(runID)+"/logs",
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if err := c.setHeaders(req); err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "text/event-stream")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	if resp.StatusCode >= 400 {
		defer resp.Body.Close()
		return nil, sdkerrors.ParseErrorResponse(resp)
	}

	return resp.Body, nil
}

// GetRunLogsText opens the plain-text log stream for a run and returns the raw
// response body as an io.ReadCloser. Each line is a human-readable log entry.
// The caller is responsible for closing it.
//
// GET /api/v1/runs/:runId/logs  (Accept: text/plain)
// Requires agents:read scope.
func (c *Client) GetRunLogsText(ctx context.Context, runID string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(
		ctx,
		"GET",
		c.base+"/api/v1/runs/"+url.PathEscape(runID)+"/logs",
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if err := c.setHeaders(req); err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "text/plain")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	if resp.StatusCode >= 400 {
		defer resp.Body.Close()
		return nil, sdkerrors.ParseErrorResponse(resp)
	}

	return resp.Body, nil
}

// --- Webhook Hook Methods ---

// CreateWebhookHook creates a new webhook hook for an agent.
// POST /api/projects/:projectId/agents/:id/hooks
// Returns 201 on success. The plaintext token is only returned once.
// Requires project:write scope.
func (c *Client) CreateWebhookHook(ctx context.Context, agentID string, createReq *CreateWebhookHookRequest) (*APIResponse[WebhookHook], error) {
	body, err := json.Marshal(createReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(
		ctx,
		"POST",
		c.base+"/api/projects/"+url.PathEscape(c.projectID)+"/agents/"+url.PathEscape(agentID)+"/hooks",
		bytes.NewReader(body),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if err := c.setHeaders(req); err != nil {
		return nil, err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, sdkerrors.ParseErrorResponse(resp)
	}

	var result APIResponse[WebhookHook]
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// ListWebhookHooks lists all webhook hooks for an agent.
// GET /api/projects/:projectId/agents/:id/hooks
// Requires project:read scope.
func (c *Client) ListWebhookHooks(ctx context.Context, agentID string) (*APIResponse[[]WebhookHook], error) {
	req, err := http.NewRequestWithContext(
		ctx,
		"GET",
		c.base+"/api/projects/"+url.PathEscape(c.projectID)+"/agents/"+url.PathEscape(agentID)+"/hooks",
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if err := c.setHeaders(req); err != nil {
		return nil, err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, sdkerrors.ParseErrorResponse(resp)
	}

	var result APIResponse[[]WebhookHook]
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// DeleteWebhookHook deletes a webhook hook.
// DELETE /api/projects/:projectId/agents/:id/hooks/:hookId
// Requires project:write scope.
func (c *Client) DeleteWebhookHook(ctx context.Context, agentID, hookID string) error {
	req, err := http.NewRequestWithContext(
		ctx,
		"DELETE",
		c.base+"/api/projects/"+url.PathEscape(c.projectID)+"/agents/"+url.PathEscape(agentID)+"/hooks/"+url.PathEscape(hookID),
		nil,
	)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	if err := c.setHeaders(req); err != nil {
		return err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return sdkerrors.ParseErrorResponse(resp)
	}

	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}

// --- ADK Sessions ---

// ADKEvent represents an event within an ADK session.
type ADKEvent struct {
	ID                     string         `json:"id"`
	SessionID              string         `json:"sessionId"`
	InvocationID           string         `json:"invocationId,omitempty"`
	Author                 string         `json:"author,omitempty"`
	Timestamp              time.Time      `json:"timestamp"`
	Branch                 *string        `json:"branch,omitempty"`
	Actions                map[string]any `json:"actions,omitempty"`
	LongRunningToolIDsJSON map[string]any `json:"longRunningToolIds,omitempty"`
	Content                map[string]any `json:"content,omitempty"`
	Partial                *bool          `json:"partial,omitempty"`
	TurnComplete           *bool          `json:"turnComplete,omitempty"`
	ErrorCode              *string        `json:"errorCode,omitempty"`
	ErrorMessage           *string        `json:"errorMessage,omitempty"`
	Interrupted            *bool          `json:"interrupted,omitempty"`
}

// ADKSession represents an ADK session.
type ADKSession struct {
	ID         string         `json:"id"`
	AppName    string         `json:"appName"`
	UserID     string         `json:"userId"`
	State      map[string]any `json:"state,omitempty"`
	CreateTime time.Time      `json:"createTime"`
	UpdateTime time.Time      `json:"updateTime"`
	Events     []*ADKEvent    `json:"events,omitempty"`
}

// ListADKSessions returns all ADK sessions for the given project.
func (c *Client) ListADKSessions(ctx context.Context, projectID string) ([]*ADKSession, error) {
	if projectID == "" {
		projectID = c.projectID
	}
	if projectID == "" {
		return nil, fmt.Errorf("projectID is required")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/api/projects/%s/adk-sessions", c.base, projectID), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	var res struct {
		Data       []*ADKSession `json:"items"`
		TotalCount int           `json:"totalCount"`
	}

	if err := doRequest(c, req, &res); err != nil {
		return nil, err
	}

	return res.Data, nil
}

// GetADKSession returns a specific ADK session and its events.
func (c *Client) GetADKSession(ctx context.Context, projectID, sessionID string) (*ADKSession, error) {
	if projectID == "" {
		projectID = c.projectID
	}
	if projectID == "" {
		return nil, fmt.Errorf("projectID is required")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/api/projects/%s/adk-sessions/%s", c.base, projectID, sessionID), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	var res struct {
		Data *ADKSession `json:"data"`
	}

	if err := doRequest(c, req, &res); err != nil {
		return nil, err
	}

	return res.Data, nil
}
func doRequest(c *Client, req *http.Request, v interface{}) error {
	if err := c.setHeaders(req); err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return sdkerrors.ParseErrorResponse(resp)
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

// --- Full trace, stats, and session stats types ---

// AgentRunFull bundles run + messages + toolCalls + optional parentRun.
type AgentRunFull struct {
	Run       *AgentRun          `json:"run"`
	Messages  []AgentRunMessage  `json:"messages"`
	ToolCalls []AgentRunToolCall `json:"toolCalls"`
	ParentRun *AgentRun          `json:"parentRun,omitempty"`
}

// RunStatsOverview holds aggregate run counts and cost.
type RunStatsOverview struct {
	TotalRuns     int64   `json:"totalRuns"`
	SuccessCount  int64   `json:"successCount"`
	FailedCount   int64   `json:"failedCount"`
	ErrorCount    int64   `json:"errorCount"`
	SuccessRate   float64 `json:"successRate"`
	AvgDurationMs float64 `json:"avgDurationMs"`
	TotalCostUSD  float64 `json:"totalCostUsd"`
}

// RunStatsAgent holds per-agent aggregated metrics.
type RunStatsAgent struct {
	Total           int64   `json:"total"`
	Success         int64   `json:"success"`
	Failed          int64   `json:"failed"`
	Errored         int64   `json:"errored"`
	AvgDurationMs   float64 `json:"avgDurationMs"`
	MaxDurationMs   int64   `json:"maxDurationMs"`
	AvgCostUSD      float64 `json:"avgCostUsd"`
	TotalCostUSD    float64 `json:"totalCostUsd"`
	AvgInputTokens  float64 `json:"avgInputTokens"`
	AvgOutputTokens float64 `json:"avgOutputTokens"`
}

// RunStatsTool holds per-tool aggregated metrics.
type RunStatsTool struct {
	Total         int64   `json:"total"`
	Success       int64   `json:"success"`
	Failed        int64   `json:"failed"`
	AvgDurationMs float64 `json:"avgDurationMs"`
	MaxDurationMs int64   `json:"maxDurationMs"`
}

// RunStatsError is an entry in the topErrors list.
type RunStatsError struct {
	Message string `json:"message"`
	Count   int64  `json:"count"`
}

// RunStatsTimePoint is a single hourly bucket in the time series.
type RunStatsTimePoint struct {
	Hour    time.Time        `json:"hour"`
	Runs    int64            `json:"runs"`
	ByAgent map[string]int64 `json:"byAgent,omitempty"`
}

// RunStats is the full response for GetProjectRunStats.
type RunStats struct {
	Period     RunStatsPeriod           `json:"period"`
	Overview   RunStatsOverview         `json:"overview"`
	ByAgent    map[string]RunStatsAgent `json:"byAgent"`
	TopErrors  []RunStatsError          `json:"topErrors"`
	ToolStats  RunStatsTools            `json:"toolStats"`
	TimeSeries RunStatsTimeSeries       `json:"timeSeries"`
}

// RunStatsPeriod captures the analysis window.
type RunStatsPeriod struct {
	Since time.Time `json:"since"`
	Until time.Time `json:"until"`
}

// RunStatsTools holds aggregate tool call statistics.
type RunStatsTools struct {
	TotalToolCalls int64                   `json:"totalToolCalls"`
	ByTool         map[string]RunStatsTool `json:"byTool"`
}

// RunStatsTimeSeries holds hourly run counts.
type RunStatsTimeSeries struct {
	ByHour []RunStatsTimePoint `json:"byHour"`
}

// RunSessionStats is the full response for GetProjectRunSessionStats.
type RunSessionStats struct {
	Period             RunStatsPeriod      `json:"period"`
	TotalSessions      int64               `json:"totalSessions"`
	ActiveSessions     int64               `json:"activeSessions"`
	AvgRunsPerSession  float64             `json:"avgRunsPerSession"`
	MaxRunsPerSession  int64               `json:"maxRunsPerSession"`
	SessionsByPlatform map[string]int64    `json:"sessionsByPlatform"`
	TopSessions        []RunSessionSummary `json:"topSessions"`
}

// RunSessionSummary summarises a single logical session.
type RunSessionSummary struct {
	Platform      string    `json:"platform"`
	ChannelID     string    `json:"channelId,omitempty"`
	ThreadID      string    `json:"threadId,omitempty"`
	TotalRuns     int64     `json:"totalRuns"`
	LastRunAt     time.Time `json:"lastRunAt"`
	AvgDurationMs float64   `json:"avgDurationMs"`
	TotalCostUSD  float64   `json:"totalCostUsd"`
}

// RunStatsOptions holds query parameters for GetProjectRunStats / GetProjectRunSessionStats.
type RunStatsOptions struct {
	Since    *time.Time
	Until    *time.Time
	AgentID  string
	Platform string // session stats only
	TopN     int    // session stats only; default 20
}

// GetProjectRunFull calls GET /api/projects/:projectId/agent-runs/:runId/full
// and returns run + messages + toolCalls + optional parentRun in one request.
func (c *Client) GetProjectRunFull(ctx context.Context, projectID, runID string) (*APIResponse[AgentRunFull], error) {
	req, err := http.NewRequestWithContext(
		ctx,
		"GET",
		c.base+"/api/projects/"+url.PathEscape(projectID)+"/agent-runs/"+url.PathEscape(runID)+"/full",
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	if err := c.setHeaders(req); err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, sdkerrors.ParseErrorResponse(resp)
	}
	var result APIResponse[AgentRunFull]
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return &result, nil
}

// GetProjectRunStats calls GET /api/projects/:projectId/agent-runs/stats
// and returns aggregate analytics for the project.
func (c *Client) GetProjectRunStats(ctx context.Context, projectID string, opts *RunStatsOptions) (*APIResponse[RunStats], error) {
	u, _ := url.Parse(c.base + "/api/projects/" + url.PathEscape(projectID) + "/agent-runs/stats")
	q := u.Query()
	if opts != nil {
		if opts.Since != nil {
			q.Set("since", opts.Since.Format(time.RFC3339))
		}
		if opts.Until != nil {
			q.Set("until", opts.Until.Format(time.RFC3339))
		}
		if opts.AgentID != "" {
			q.Set("agentId", opts.AgentID)
		}
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	if err := c.setHeaders(req); err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, sdkerrors.ParseErrorResponse(resp)
	}
	var result APIResponse[RunStats]
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return &result, nil
}

// GetProjectRunSessionStats calls GET /api/projects/:projectId/agent-runs/stats/sessions
// and returns session-level analytics grouped by trigger metadata.
func (c *Client) GetProjectRunSessionStats(ctx context.Context, projectID string, opts *RunStatsOptions) (*APIResponse[RunSessionStats], error) {
	u, _ := url.Parse(c.base + "/api/projects/" + url.PathEscape(projectID) + "/agent-runs/stats/sessions")
	q := u.Query()
	if opts != nil {
		if opts.Since != nil {
			q.Set("since", opts.Since.Format(time.RFC3339))
		}
		if opts.Until != nil {
			q.Set("until", opts.Until.Format(time.RFC3339))
		}
		if opts.Platform != "" {
			q.Set("platform", opts.Platform)
		}
		if opts.TopN > 0 {
			q.Set("topN", fmt.Sprintf("%d", opts.TopN))
		}
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	if err := c.setHeaders(req); err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, sdkerrors.ParseErrorResponse(resp)
	}
	var result APIResponse[RunSessionStats]
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return &result, nil
}
