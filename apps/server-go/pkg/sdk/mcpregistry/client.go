// Package mcpregistry provides the MCP Server Registry client for the Emergent API SDK.
// It manages registered MCP servers (CRUD, tool sync, inspect) via the admin REST API.
// Requires authentication with admin:read and/or admin:write scopes.
package mcpregistry

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

	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk/auth"
	sdkerrors "github.com/emergent-company/emergent/apps/server-go/pkg/sdk/errors"
)

// Client provides access to the MCP Server Registry API.
type Client struct {
	http      *http.Client
	base      string
	auth      auth.Provider
	mu        sync.RWMutex
	orgID     string
	projectID string
}

// NewClient creates a new MCP registry client.
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

// MCPServerType defines the transport type for an MCP server.
type MCPServerType string

const (
	ServerTypeBuiltin MCPServerType = "builtin"
	ServerTypeStdio   MCPServerType = "stdio"
	ServerTypeSSE     MCPServerType = "sse"
	ServerTypeHTTP    MCPServerType = "http"
)

// MCPServer is the response DTO for an MCP server.
type MCPServer struct {
	ID        string         `json:"id"`
	ProjectID string         `json:"projectId"`
	Name      string         `json:"name"`
	Enabled   bool           `json:"enabled"`
	Type      MCPServerType  `json:"type"`
	Command   *string        `json:"command,omitempty"`
	Args      []string       `json:"args,omitempty"`
	Env       map[string]any `json:"env,omitempty"`
	URL       *string        `json:"url,omitempty"`
	Headers   map[string]any `json:"headers,omitempty"`
	ToolCount int            `json:"toolCount"`
	CreatedAt time.Time      `json:"createdAt"`
	UpdatedAt time.Time      `json:"updatedAt"`
}

// MCPServerTool is the response DTO for an MCP server tool.
type MCPServerTool struct {
	ID          string         `json:"id"`
	ServerID    string         `json:"serverId"`
	ToolName    string         `json:"toolName"`
	Description *string        `json:"description,omitempty"`
	InputSchema map[string]any `json:"inputSchema,omitempty"`
	Enabled     bool           `json:"enabled"`
	CreatedAt   time.Time      `json:"createdAt"`
}

// MCPServerDetail includes the server and its tools.
type MCPServerDetail struct {
	MCPServer
	Tools []MCPServerTool `json:"tools"`
}

// CreateMCPServerRequest is the request body for creating an MCP server.
type CreateMCPServerRequest struct {
	Name    string         `json:"name"`
	Type    MCPServerType  `json:"type"`
	Enabled *bool          `json:"enabled,omitempty"`
	Command *string        `json:"command,omitempty"`
	Args    []string       `json:"args,omitempty"`
	Env     map[string]any `json:"env,omitempty"`
	URL     *string        `json:"url,omitempty"`
	Headers map[string]any `json:"headers,omitempty"`
}

// UpdateMCPServerRequest is the request body for updating an MCP server.
type UpdateMCPServerRequest struct {
	Name    *string        `json:"name,omitempty"`
	Enabled *bool          `json:"enabled,omitempty"`
	Command *string        `json:"command,omitempty"`
	Args    []string       `json:"args,omitempty"`
	Env     map[string]any `json:"env,omitempty"`
	URL     *string        `json:"url,omitempty"`
	Headers map[string]any `json:"headers,omitempty"`
}

// MCPServerInspect is the response for the inspect/test-connection endpoint.
type MCPServerInspect struct {
	ServerID   string        `json:"serverId"`
	ServerName string        `json:"serverName"`
	ServerType MCPServerType `json:"serverType"`

	Status    string  `json:"status"` // "ok" or "error"
	Error     *string `json:"error,omitempty"`
	LatencyMs int64   `json:"latencyMs"`

	ServerInfo   *InspectServerInfo   `json:"serverInfo,omitempty"`
	Capabilities *InspectCapabilities `json:"capabilities,omitempty"`

	Tools             []InspectTool             `json:"tools"`
	Prompts           []InspectPrompt           `json:"prompts"`
	Resources         []InspectResource         `json:"resources"`
	ResourceTemplates []InspectResourceTemplate `json:"resourceTemplates"`
}

// InspectServerInfo carries the server's self-reported identity.
type InspectServerInfo struct {
	Name            string `json:"name"`
	Version         string `json:"version"`
	ProtocolVersion string `json:"protocolVersion"`
	Instructions    string `json:"instructions,omitempty"`
}

// InspectCapabilities is a simplified view of what the server supports.
type InspectCapabilities struct {
	Tools       bool `json:"tools"`
	Prompts     bool `json:"prompts"`
	Resources   bool `json:"resources"`
	Logging     bool `json:"logging"`
	Completions bool `json:"completions"`
}

// InspectTool describes a tool discovered during inspect.
type InspectTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"inputSchema,omitempty"`
}

// InspectPrompt describes a prompt discovered during inspect.
type InspectPrompt struct {
	Name        string             `json:"name"`
	Description string             `json:"description,omitempty"`
	Arguments   []InspectPromptArg `json:"arguments,omitempty"`
}

// InspectPromptArg describes a prompt argument.
type InspectPromptArg struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required"`
}

// InspectResource describes a resource discovered during inspect.
type InspectResource struct {
	URI         string `json:"uri"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	MimeType    string `json:"mimeType,omitempty"`
}

// InspectResourceTemplate describes a resource template discovered during inspect.
type InspectResourceTemplate struct {
	URITemplate string `json:"uriTemplate"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	MimeType    string `json:"mimeType,omitempty"`
}

// APIResponse wraps API responses with success flag.
type APIResponse[T any] struct {
	Success bool    `json:"success"`
	Data    T       `json:"data,omitempty"`
	Error   *string `json:"error,omitempty"`
	Message *string `json:"message,omitempty"`
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

// List returns all MCP servers for the current project.
// GET /api/admin/mcp-servers
func (c *Client) List(ctx context.Context) (*APIResponse[[]MCPServer], error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.base+"/api/admin/mcp-servers", nil)
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

	var result APIResponse[[]MCPServer]
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// Get returns an MCP server by ID, including its tools.
// GET /api/admin/mcp-servers/:id
func (c *Client) Get(ctx context.Context, serverID string) (*APIResponse[MCPServerDetail], error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.base+"/api/admin/mcp-servers/"+url.PathEscape(serverID), nil)
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

	var result APIResponse[MCPServerDetail]
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// Create creates a new MCP server.
// POST /api/admin/mcp-servers
func (c *Client) Create(ctx context.Context, createReq *CreateMCPServerRequest) (*APIResponse[MCPServer], error) {
	body, err := json.Marshal(createReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.base+"/api/admin/mcp-servers", bytes.NewReader(body))
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

	var result APIResponse[MCPServer]
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// Update updates an existing MCP server (partial update via PATCH).
// PATCH /api/admin/mcp-servers/:id
func (c *Client) Update(ctx context.Context, serverID string, updateReq *UpdateMCPServerRequest) (*APIResponse[MCPServer], error) {
	body, err := json.Marshal(updateReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "PATCH", c.base+"/api/admin/mcp-servers/"+url.PathEscape(serverID), bytes.NewReader(body))
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

	var result APIResponse[MCPServer]
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// Delete deletes an MCP server by ID.
// DELETE /api/admin/mcp-servers/:id
func (c *Client) Delete(ctx context.Context, serverID string) error {
	req, err := http.NewRequestWithContext(ctx, "DELETE", c.base+"/api/admin/mcp-servers/"+url.PathEscape(serverID), nil)
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

// SyncTools triggers tool discovery and sync for an MCP server.
// POST /api/admin/mcp-servers/:id/sync
// Returns the message from the server (e.g., "synced 5 tools successfully").
func (c *Client) SyncTools(ctx context.Context, serverID string) (*APIResponse[any], error) {
	req, err := http.NewRequestWithContext(ctx, "POST", c.base+"/api/admin/mcp-servers/"+url.PathEscape(serverID)+"/sync", nil)
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

	var result APIResponse[any]
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// Inspect performs a diagnostic test-connection to an MCP server.
// POST /api/admin/mcp-servers/:id/inspect
func (c *Client) Inspect(ctx context.Context, serverID string) (*APIResponse[MCPServerInspect], error) {
	req, err := http.NewRequestWithContext(ctx, "POST", c.base+"/api/admin/mcp-servers/"+url.PathEscape(serverID)+"/inspect", nil)
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

	var result APIResponse[MCPServerInspect]
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// ListTools returns all tools for an MCP server.
// GET /api/admin/mcp-servers/:id/tools
func (c *Client) ListTools(ctx context.Context, serverID string) (*APIResponse[[]MCPServerTool], error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.base+"/api/admin/mcp-servers/"+url.PathEscape(serverID)+"/tools", nil)
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

	var result APIResponse[[]MCPServerTool]
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// ToggleTool enables or disables a specific tool on an MCP server.
// PATCH /api/admin/mcp-servers/:id/tools/:toolId
func (c *Client) ToggleTool(ctx context.Context, serverID, toolID string, enabled bool) error {
	payload := map[string]any{"enabled": enabled}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "PATCH",
		c.base+"/api/admin/mcp-servers/"+url.PathEscape(serverID)+"/tools/"+url.PathEscape(toolID),
		bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
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
