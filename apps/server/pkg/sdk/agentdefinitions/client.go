// Package agentdefinitions provides the Agent Definitions service client for the Emergent API SDK.
// Agent definitions store agent configurations (system prompts, tools, model config, flow type, visibility)
// separately from runtime agent state. Requires authentication with admin:read and/or admin:write scopes.
package agentdefinitions

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

// Client provides access to the Agent Definitions API.
type Client struct {
	http      *http.Client
	base      string
	auth      auth.Provider
	mu        sync.RWMutex
	orgID     string
	projectID string
}

// NewClient creates a new agent definitions client.
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

// AgentDefinition represents a full agent definition.
type AgentDefinition struct {
	ID             string         `json:"id"`
	ProductID      *string        `json:"productId,omitempty"`
	ProjectID      string         `json:"projectId"`
	Name           string         `json:"name"`
	Description    *string        `json:"description,omitempty"`
	SystemPrompt   *string        `json:"systemPrompt,omitempty"`
	Model          *ModelConfig   `json:"model,omitempty"`
	Tools          []string       `json:"tools"`
	FlowType       string         `json:"flowType"`
	IsDefault      bool           `json:"isDefault"`
	MaxSteps       *int           `json:"maxSteps,omitempty"`
	DefaultTimeout *int           `json:"defaultTimeout,omitempty"`
	Visibility     string         `json:"visibility"`
	DispatchMode   string         `json:"dispatchMode,omitempty"`
	ACPConfig      *ACPConfig     `json:"acpConfig,omitempty"`
	Config         map[string]any `json:"config,omitempty"`
	CreatedAt      time.Time      `json:"createdAt"`
	UpdatedAt      time.Time      `json:"updatedAt"`
}

// AgentDefinitionSummary is a lightweight representation for list responses.
type AgentDefinitionSummary struct {
	ID          string    `json:"id"`
	ProjectID   string    `json:"projectId"`
	Name        string    `json:"name"`
	Description *string   `json:"description,omitempty"`
	FlowType    string    `json:"flowType"`
	Visibility  string    `json:"visibility"`
	IsDefault   bool      `json:"isDefault"`
	ToolCount   int       `json:"toolCount"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// ModelConfig contains model configuration for an agent definition.
type ModelConfig struct {
	Name        string   `json:"name,omitempty"`
	Temperature *float32 `json:"temperature,omitempty"`
	MaxTokens   *int     `json:"maxTokens,omitempty"`
}

// ACPConfig contains Agent Card Protocol metadata.
type ACPConfig struct {
	DisplayName  string   `json:"displayName,omitempty"`
	Description  string   `json:"description,omitempty"`
	Capabilities []string `json:"capabilities,omitempty"`
	InputModes   []string `json:"inputModes,omitempty"`
	OutputModes  []string `json:"outputModes,omitempty"`
}

// APIResponse wraps API responses with success flag.
type APIResponse[T any] struct {
	Success bool    `json:"success"`
	Data    T       `json:"data,omitempty"`
	Error   *string `json:"error,omitempty"`
	Message *string `json:"message,omitempty"`
}

// CreateAgentDefinitionRequest is the request body for creating an agent definition.
type CreateAgentDefinitionRequest struct {
	Name           string         `json:"name"`
	Description    *string        `json:"description,omitempty"`
	SystemPrompt   *string        `json:"systemPrompt,omitempty"`
	Model          *ModelConfig   `json:"model,omitempty"`
	Tools          []string       `json:"tools,omitempty"`
	FlowType       string         `json:"flowType,omitempty"`
	IsDefault      *bool          `json:"isDefault,omitempty"`
	MaxSteps       *int           `json:"maxSteps,omitempty"`
	DefaultTimeout *int           `json:"defaultTimeout,omitempty"`
	Visibility     string         `json:"visibility,omitempty"`
	DispatchMode   string         `json:"dispatchMode,omitempty"`
	ACPConfig      *ACPConfig     `json:"acpConfig,omitempty"`
	Config         map[string]any `json:"config,omitempty"`
}

// UpdateAgentDefinitionRequest is the request body for updating an agent definition.
type UpdateAgentDefinitionRequest struct {
	Name           *string        `json:"name,omitempty"`
	Description    *string        `json:"description,omitempty"`
	SystemPrompt   *string        `json:"systemPrompt,omitempty"`
	Model          *ModelConfig   `json:"model,omitempty"`
	Tools          []string       `json:"tools,omitempty"`
	FlowType       *string        `json:"flowType,omitempty"`
	IsDefault      *bool          `json:"isDefault,omitempty"`
	MaxSteps       *int           `json:"maxSteps,omitempty"`
	DefaultTimeout *int           `json:"defaultTimeout,omitempty"`
	Visibility     *string        `json:"visibility,omitempty"`
	DispatchMode   *string        `json:"dispatchMode,omitempty"`
	ACPConfig      *ACPConfig     `json:"acpConfig,omitempty"`
	Config         map[string]any `json:"config,omitempty"`
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

// projectPath returns the project-scoped base path for agent definition API calls.
func (c *Client) projectPath() string {
	c.mu.RLock()
	pid := c.projectID
	c.mu.RUnlock()
	return c.base + "/api/projects/" + url.PathEscape(pid)
}

// --- API Methods ---

// List returns all agent definitions for the current project.
// GET /api/projects/:projectId/agent-definitions
func (c *Client) List(ctx context.Context) (*APIResponse[[]AgentDefinitionSummary], error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.projectPath()+"/agent-definitions", nil)
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

	var result APIResponse[[]AgentDefinitionSummary]
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// Get returns an agent definition by ID.
// GET /api/projects/:projectId/agent-definitions/:id
func (c *Client) Get(ctx context.Context, definitionID string) (*APIResponse[AgentDefinition], error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.projectPath()+"/agent-definitions/"+url.PathEscape(definitionID), nil)
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

	var result APIResponse[AgentDefinition]
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// Create creates a new agent definition.
// POST /api/projects/:projectId/agent-definitions
// Returns 201 on success.
func (c *Client) Create(ctx context.Context, createReq *CreateAgentDefinitionRequest) (*APIResponse[AgentDefinition], error) {
	body, err := json.Marshal(createReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.projectPath()+"/agent-definitions", bytes.NewReader(body))
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

	var result APIResponse[AgentDefinition]
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// Update updates an agent definition (partial update via PATCH).
// PATCH /api/projects/:projectId/agent-definitions/:id
func (c *Client) Update(ctx context.Context, definitionID string, updateReq *UpdateAgentDefinitionRequest) (*APIResponse[AgentDefinition], error) {
	body, err := json.Marshal(updateReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "PATCH", c.projectPath()+"/agent-definitions/"+url.PathEscape(definitionID), bytes.NewReader(body))
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

	var result APIResponse[AgentDefinition]
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// Delete deletes an agent definition by ID.
// DELETE /api/projects/:projectId/agent-definitions/:id
func (c *Client) Delete(ctx context.Context, definitionID string) error {
	req, err := http.NewRequestWithContext(ctx, "DELETE", c.projectPath()+"/agent-definitions/"+url.PathEscape(definitionID), nil)
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

// --- Agent Override Types ---

// AgentOverride represents a partial agent definition override.
// Fields that are nil/empty are not overridden — they inherit the canonical defaults.
type AgentOverride struct {
	SystemPrompt  *string        `json:"systemPrompt,omitempty"`
	Model         *ModelConfig   `json:"model,omitempty"`
	Tools         []string       `json:"tools,omitempty"`
	MaxSteps      *int           `json:"maxSteps,omitempty"`
	SandboxConfig map[string]any `json:"sandboxConfig,omitempty"`
}

// OverrideEntry is a single agent override as returned by the list endpoint.
type OverrideEntry struct {
	AgentName string         `json:"agentName"`
	Override  map[string]any `json:"override"`
}

// --- Agent Override Methods ---

// ListOverrides returns all agent overrides for the current project.
// GET /api/projects/:projectId/agent-definitions/overrides
func (c *Client) ListOverrides(ctx context.Context) (*APIResponse[[]OverrideEntry], error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.projectPath()+"/agent-definitions/overrides", nil)
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

	var result APIResponse[[]OverrideEntry]
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// GetOverride returns the agent override for a specific agent name.
// GET /api/projects/:projectId/agent-definitions/overrides/:agentName
func (c *Client) GetOverride(ctx context.Context, agentName string) (*APIResponse[AgentOverride], error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.projectPath()+"/agent-definitions/overrides/"+url.PathEscape(agentName), nil)
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

	var result APIResponse[AgentOverride]
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// SetOverride creates or updates the agent override for a specific agent name.
// PUT /api/projects/:projectId/agent-definitions/overrides/:agentName
func (c *Client) SetOverride(ctx context.Context, agentName string, override *AgentOverride) (*APIResponse[map[string]any], error) {
	body, err := json.Marshal(override)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "PUT", c.projectPath()+"/agent-definitions/overrides/"+url.PathEscape(agentName), bytes.NewReader(body))
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

	var result APIResponse[map[string]any]
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// DeleteOverride removes the agent override, reverting to canonical defaults.
// DELETE /api/projects/:projectId/agent-definitions/overrides/:agentName
func (c *Client) DeleteOverride(ctx context.Context, agentName string) error {
	req, err := http.NewRequestWithContext(ctx, "DELETE", c.projectPath()+"/agent-definitions/overrides/"+url.PathEscape(agentName), nil)
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
