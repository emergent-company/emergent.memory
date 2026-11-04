package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	sdkerrors "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/errors"
)

// ============================================================================
// DTOs — agent-owned MCP endpoint, its labeled keys, and its sessions.
//
// These mirror the server's project-admin DTOs (apps/server/domain/mcp). The
// read shapes deliberately carry no secret: only AgentMCPKeySecret, returned by
// key create and key rotate, contains the raw token value.
// ============================================================================

// AgentMCPEndpoint is the non-secret representation of an agent's MCP endpoint.
type AgentMCPEndpoint struct {
	ID        string     `json:"id"`
	ProjectID string     `json:"projectId"`
	AgentID   string     `json:"agentId"`
	Status    string     `json:"status"`
	CreatedAt time.Time  `json:"createdAt"`
	UpdatedAt time.Time  `json:"updatedAt"`
	RevokedAt *time.Time `json:"revokedAt,omitempty"`
	MCPURL    string     `json:"mcpUrl"`
}

// AgentMCPKey is the non-secret representation of one endpoint key.
type AgentMCPKey struct {
	ID         string     `json:"id"`
	EndpointID string     `json:"endpointId"`
	AgentID    string     `json:"agentId"`
	Label      string     `json:"label"`
	Status     string     `json:"status"`
	CreatedAt  time.Time  `json:"createdAt"`
	UpdatedAt  time.Time  `json:"updatedAt"`
	LastUsedAt *time.Time `json:"lastUsedAt,omitempty"`
	ExpiresAt  *time.Time `json:"expiresAt,omitempty"`
}

// AgentMCPKeyList is the response for the list-keys endpoint.
type AgentMCPKeyList struct {
	Keys  []AgentMCPKey `json:"keys"`
	Total int           `json:"total"`
}

// CreateAgentMCPKeyRequest is the request body for POST .../keys. Label is
// required; ExpiresAt optionally maps onto the bound token's expiry.
type CreateAgentMCPKeyRequest struct {
	Label     string     `json:"label"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
}

// AgentMCPKeySecret is returned by key create and key rotate. It carries the raw
// token value exactly once; the server never returns it again.
type AgentMCPKeySecret struct {
	AgentMCPKey
	Token  string `json:"token"`
	MCPURL string `json:"mcpUrl"`
}

// AgentMCPSession is one row of the endpoint's project-admin session list. It
// exposes lifecycle/counter metadata only — never message content or credential
// material.
type AgentMCPSession struct {
	SessionID    string     `json:"session_id"`
	KeyID        string     `json:"key_id"`
	KeyLabel     string     `json:"key_label"`
	Status       string     `json:"status"`
	TurnCount    int        `json:"turn_count"`
	TotalSteps   int        `json:"total_steps"`
	CreatedAt    time.Time  `json:"created_at"`
	LastActiveAt time.Time  `json:"last_active_at"`
	ExpiresAt    *time.Time `json:"expires_at"`
}

// AgentMCPSessionList is the response for the list-sessions endpoint. Sessions
// is always non-null, including when the endpoint has no sessions.
type AgentMCPSessionList struct {
	Sessions []AgentMCPSession `json:"sessions"`
}

// ============================================================================
// Request plumbing
// ============================================================================

// agentMCPProjectPath is the project-scoped path prefix shared by the agent MCP
// routes.
func agentMCPProjectPath(projectID string) string {
	return "/api/projects/" + url.PathEscape(projectID)
}

// doJSON executes an authenticated JSON request and decodes the response into
// out (nil out discards the body). Error responses are parsed into the SDK's
// typed *sdkerrors.Error so callers can branch on status code.
func (c *Client) doJSON(ctx context.Context, method, path string, body, out interface{}) error {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("failed to marshal request: %w", err)
		}
		reader = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.base+path, reader)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	if err := c.auth.Authenticate(req); err != nil {
		return fmt.Errorf("authentication failed: %w", err)
	}

	c.mu.RLock()
	projectID := c.projectID
	c.mu.RUnlock()
	if projectID != "" {
		req.Header.Set("X-Project-ID", projectID)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return sdkerrors.ParseErrorResponse(resp)
	}

	if out == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}
	return nil
}

// ============================================================================
// Endpoint lifecycle
// ============================================================================

// CreateAgentEndpoint establishes the single active MCP endpoint owned by an
// agent. It returns a 409 conflict when an active endpoint already exists.
func (c *Client) CreateAgentEndpoint(ctx context.Context, projectID, agentID string) (*AgentMCPEndpoint, error) {
	path := fmt.Sprintf("%s/agents/%s/mcp-endpoint", agentMCPProjectPath(projectID), url.PathEscape(agentID))
	var out AgentMCPEndpoint
	if err := c.doJSON(ctx, http.MethodPost, path, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetAgentEndpoint returns an agent's active MCP endpoint. It returns a 404 when
// the agent has no active endpoint.
func (c *Client) GetAgentEndpoint(ctx context.Context, projectID, agentID string) (*AgentMCPEndpoint, error) {
	path := fmt.Sprintf("%s/agents/%s/mcp-endpoint", agentMCPProjectPath(projectID), url.PathEscape(agentID))
	var out AgentMCPEndpoint
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// RevokeAgentEndpoint revokes an endpoint and its remaining active keys,
// invalidating their tokens. It is idempotent.
func (c *Client) RevokeAgentEndpoint(ctx context.Context, projectID, endpointID string) error {
	path := fmt.Sprintf("%s/agent-mcp-endpoints/%s", agentMCPProjectPath(projectID), url.PathEscape(endpointID))
	return c.doJSON(ctx, http.MethodDelete, path, nil, nil)
}

// ============================================================================
// Key lifecycle
// ============================================================================

// CreateAgentKey mints a labeled credential bound to an endpoint. The response
// carries the raw token value exactly once.
func (c *Client) CreateAgentKey(ctx context.Context, projectID, endpointID string, req CreateAgentMCPKeyRequest) (*AgentMCPKeySecret, error) {
	path := fmt.Sprintf("%s/agent-mcp-endpoints/%s/keys", agentMCPProjectPath(projectID), url.PathEscape(endpointID))
	var out AgentMCPKeySecret
	if err := c.doJSON(ctx, http.MethodPost, path, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListAgentKeys returns an endpoint's keys without secrets.
func (c *Client) ListAgentKeys(ctx context.Context, projectID, endpointID string) (*AgentMCPKeyList, error) {
	path := fmt.Sprintf("%s/agent-mcp-endpoints/%s/keys", agentMCPProjectPath(projectID), url.PathEscape(endpointID))
	var out AgentMCPKeyList
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// RevokeAgentKey revokes one key without touching its endpoint or sibling keys.
// It is idempotent.
func (c *Client) RevokeAgentKey(ctx context.Context, projectID, keyID string) error {
	path := fmt.Sprintf("%s/agent-mcp-keys/%s", agentMCPProjectPath(projectID), url.PathEscape(keyID))
	return c.doJSON(ctx, http.MethodDelete, path, nil, nil)
}

// RotateAgentKey issues a replacement token for the same key and endpoint,
// invalidating the previous token while preserving the key identity. The
// response carries the new raw token value exactly once.
func (c *Client) RotateAgentKey(ctx context.Context, projectID, keyID string) (*AgentMCPKeySecret, error) {
	path := fmt.Sprintf("%s/agent-mcp-keys/%s/rotate", agentMCPProjectPath(projectID), url.PathEscape(keyID))
	var out AgentMCPKeySecret
	if err := c.doJSON(ctx, http.MethodPost, path, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ============================================================================
// Sessions
// ============================================================================

// ListAgentSessions returns an endpoint's sessions, most recently active first.
// An optional status filters by lifecycle state; the server rejects an
// unrecognized value with a 400.
func (c *Client) ListAgentSessions(ctx context.Context, projectID, endpointID, status string) (*AgentMCPSessionList, error) {
	path := fmt.Sprintf("%s/agent-mcp-endpoints/%s/sessions", agentMCPProjectPath(projectID), url.PathEscape(endpointID))
	if status != "" {
		path += "?status=" + url.QueryEscape(status)
	}
	var out AgentMCPSessionList
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
