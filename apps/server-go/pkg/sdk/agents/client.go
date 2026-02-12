// Package agents provides the Agents service client for the Emergent API SDK.
// Agents are background automation workers that run on schedules or react to events.
// Requires authentication with admin:read and/or admin:write scopes.
package agents

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk/auth"
	sdkerrors "github.com/emergent-company/emergent/apps/server-go/pkg/sdk/errors"
)

// Client provides access to the Agents API.
type Client struct {
	http      *http.Client
	base      string
	auth      auth.Provider
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

// AgentRun represents an agent run record.
type AgentRun struct {
	ID           string         `json:"id"`
	AgentID      string         `json:"agentId"`
	Status       string         `json:"status"`
	StartedAt    time.Time      `json:"startedAt"`
	CompletedAt  *time.Time     `json:"completedAt"`
	DurationMs   *int           `json:"durationMs"`
	Summary      map[string]any `json:"summary"`
	ErrorMessage *string        `json:"errorMessage"`
	SkipReason   *string        `json:"skipReason"`
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
	ProjectID      string             `json:"projectId"`
	Name           string             `json:"name"`
	StrategyType   string             `json:"strategyType"`
	Prompt         *string            `json:"prompt,omitempty"`
	CronSchedule   string             `json:"cronSchedule"`
	Enabled        *bool              `json:"enabled,omitempty"`
	TriggerType    string             `json:"triggerType,omitempty"`
	ReactionConfig *ReactionConfig    `json:"reactionConfig,omitempty"`
	ExecutionMode  string             `json:"executionMode,omitempty"`
	Capabilities   *AgentCapabilities `json:"capabilities,omitempty"`
	Config         map[string]any     `json:"config,omitempty"`
	Description    *string            `json:"description,omitempty"`
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
	Message *string `json:"message,omitempty"`
	Error   *string `json:"error,omitempty"`
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

// --- Internal helpers ---

func (c *Client) setHeaders(req *http.Request) error {
	if err := c.auth.Authenticate(req); err != nil {
		return fmt.Errorf("authentication failed: %w", err)
	}
	if c.orgID != "" {
		req.Header.Set("X-Org-ID", c.orgID)
	}
	if c.projectID != "" {
		req.Header.Set("X-Project-ID", c.projectID)
	}
	return nil
}

// --- API Methods ---

// List returns all agents for the current project.
// GET /api/admin/agents
// Requires admin:read scope.
func (c *Client) List(ctx context.Context) (*APIResponse[[]Agent], error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.base+"/api/admin/agents", nil)
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

// Get returns an agent by ID.
// GET /api/admin/agents/:id
// Requires admin:read scope.
func (c *Client) Get(ctx context.Context, agentID string) (*APIResponse[Agent], error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.base+"/api/admin/agents/"+agentID, nil)
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

// GetRuns returns recent runs for an agent.
// GET /api/admin/agents/:id/runs
// Requires admin:read scope.
func (c *Client) GetRuns(ctx context.Context, agentID string, limit int) (*APIResponse[[]AgentRun], error) {
	u, err := url.Parse(c.base + "/api/admin/agents/" + agentID + "/runs")
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
// GET /api/admin/agents/:id/pending-events
// Requires admin:read scope.
func (c *Client) GetPendingEvents(ctx context.Context, agentID string, limit int) (*APIResponse[PendingEventsResponse], error) {
	u, err := url.Parse(c.base + "/api/admin/agents/" + agentID + "/pending-events")
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
// POST /api/admin/agents
// Requires admin:write scope. Returns 201 on success.
func (c *Client) Create(ctx context.Context, createReq *CreateAgentRequest) (*APIResponse[Agent], error) {
	body, err := json.Marshal(createReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.base+"/api/admin/agents", bytes.NewReader(body))
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
// PATCH /api/admin/agents/:id
// Requires admin:write scope.
func (c *Client) Update(ctx context.Context, agentID string, updateReq *UpdateAgentRequest) (*APIResponse[Agent], error) {
	body, err := json.Marshal(updateReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "PATCH", c.base+"/api/admin/agents/"+agentID, bytes.NewReader(body))
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
// DELETE /api/admin/agents/:id
// Requires admin:write scope.
func (c *Client) Delete(ctx context.Context, agentID string) error {
	req, err := http.NewRequestWithContext(ctx, "DELETE", c.base+"/api/admin/agents/"+agentID, nil)
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
// POST /api/admin/agents/:id/trigger
// Requires admin:write scope.
func (c *Client) Trigger(ctx context.Context, agentID string) (*TriggerResponse, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", c.base+"/api/admin/agents/"+agentID+"/trigger", nil)
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

// BatchTrigger triggers a reaction agent for multiple graph objects.
// POST /api/admin/agents/:id/batch-trigger
// Requires admin:write scope. Max 100 objects per batch.
func (c *Client) BatchTrigger(ctx context.Context, agentID string, batchReq *BatchTriggerRequest) (*APIResponse[BatchTriggerResponse], error) {
	body, err := json.Marshal(batchReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.base+"/api/admin/agents/"+agentID+"/batch-trigger", bytes.NewReader(body))
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
