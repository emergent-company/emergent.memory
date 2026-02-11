// Package mcp provides the Model Context Protocol service client for the Emergent API SDK.
package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/emergent/emergent-core/pkg/sdk/auth"
	sdkerrors "github.com/emergent/emergent-core/pkg/sdk/errors"
)

// Client provides access to the MCP (Model Context Protocol) API.
type Client struct {
	http      *http.Client
	base      string
	auth      auth.Provider
	orgID     string
	projectID string
}

// NewClient creates a new MCP client.
func NewClient(httpClient *http.Client, baseURL string, authProvider auth.Provider) *Client {
	return &Client{
		http: httpClient,
		base: baseURL,
		auth: authProvider,
	}
}

// SetContext sets the organization and project context.
func (c *Client) SetContext(orgID, projectID string) {
	c.orgID = orgID
	c.projectID = projectID
}

// JSONRPCRequest represents a JSON-RPC 2.0 request
type JSONRPCRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id,omitempty"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

// JSONRPCResponse represents a JSON-RPC 2.0 response
type JSONRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *JSONRPCError   `json:"error,omitempty"`
}

// JSONRPCError represents a JSON-RPC 2.0 error
type JSONRPCError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// CallMethod calls a JSON-RPC 2.0 method on the MCP endpoint.
func (c *Client) CallMethod(ctx context.Context, method string, params interface{}) (json.RawMessage, error) {
	req := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  method,
		Params:  params,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.base+"/api/mcp/rpc", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")

	// Add authentication
	if err := c.auth.Authenticate(httpReq); err != nil {
		return nil, fmt.Errorf("authentication failed: %w", err)
	}

	// Add project context
	if c.projectID != "" {
		httpReq.Header.Set("X-Project-ID", c.projectID)
	}

	// Execute request
	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Check for HTTP errors
	if resp.StatusCode >= 400 {
		return nil, sdkerrors.ParseErrorResponse(resp)
	}

	// Parse JSON-RPC response
	var rpcResp JSONRPCResponse
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// Check for JSON-RPC error
	if rpcResp.Error != nil {
		return nil, fmt.Errorf("JSON-RPC error %d: %s", rpcResp.Error.Code, rpcResp.Error.Message)
	}

	return rpcResp.Result, nil
}

// Initialize performs MCP protocol initialization.
func (c *Client) Initialize(ctx context.Context) error {
	params := map[string]interface{}{
		"protocolVersion": "2025-11-25",
		"capabilities":    map[string]interface{}{},
		"clientInfo": map[string]string{
			"name":    "emergent-go-sdk",
			"version": "1.0.0",
		},
	}

	_, err := c.CallMethod(ctx, "initialize", params)
	return err
}

// ListTools lists available MCP tools.
func (c *Client) ListTools(ctx context.Context) (json.RawMessage, error) {
	return c.CallMethod(ctx, "tools/list", nil)
}

// CallTool calls an MCP tool with the given arguments.
func (c *Client) CallTool(ctx context.Context, name string, arguments interface{}) (json.RawMessage, error) {
	params := map[string]interface{}{
		"name":      name,
		"arguments": arguments,
	}
	return c.CallMethod(ctx, "tools/call", params)
}

// ListResources lists available MCP resources.
func (c *Client) ListResources(ctx context.Context) (json.RawMessage, error) {
	return c.CallMethod(ctx, "resources/list", nil)
}

// ReadResource reads an MCP resource by URI.
func (c *Client) ReadResource(ctx context.Context, uri string) (json.RawMessage, error) {
	params := map[string]interface{}{
		"uri": uri,
	}
	return c.CallMethod(ctx, "resources/read", params)
}

// ListPrompts lists available MCP prompts.
func (c *Client) ListPrompts(ctx context.Context) (json.RawMessage, error) {
	return c.CallMethod(ctx, "prompts/list", nil)
}

// GetPrompt retrieves an MCP prompt with arguments.
func (c *Client) GetPrompt(ctx context.Context, name string, arguments interface{}) (json.RawMessage, error) {
	params := map[string]interface{}{
		"name":      name,
		"arguments": arguments,
	}
	return c.CallMethod(ctx, "prompts/get", params)
}
