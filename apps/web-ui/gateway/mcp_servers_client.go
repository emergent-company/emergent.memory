package main

import (
	"context"
	"encoding/json"
	"net/http"
)

// --- MCP server sync / inspect / tool management ---
//
// These methods proxy the memory mcpregistry admin routes (the same
// /api/admin/mcp-servers namespace as the CRUD methods in extras.go). The
// per-request project scoping rides on the session X-Project-ID header via
// sessionHeaders.

// MCPInspectResult mirrors memory's MCPServerInspectDTO — the response of
// POST /:id/inspect. It captures everything discoverable about an MCP server
// in a single call: connection status, server metadata, capabilities, and
// enumerations of tools, prompts, and resources. Inspect always returns 200;
// a failed connection comes back in the body with Status "error" and Error set.
type MCPInspectResult struct {
	// Server identity
	ServerID   string `json:"serverId"`
	ServerName string `json:"serverName"`
	ServerType string `json:"serverType"`

	// Connection result
	Status    string  `json:"status"` // "ok" or "error"
	Error     *string `json:"error,omitempty"`
	LatencyMs int64   `json:"latencyMs"`

	// Server info from InitializeResult
	ServerInfo *MCPInspectServerInfo `json:"serverInfo,omitempty"`

	// Capabilities advertised by the server
	Capabilities *MCPInspectCapabilities `json:"capabilities,omitempty"`

	// Enumerated items (only populated when the server advertises the capability)
	Tools             []MCPInspectTool             `json:"tools"`
	Prompts           []MCPInspectPrompt           `json:"prompts"`
	Resources         []MCPInspectResource         `json:"resources"`
	ResourceTemplates []MCPInspectResourceTemplate `json:"resourceTemplates"`
}

// MCPInspectServerInfo carries the server's self-reported identity.
type MCPInspectServerInfo struct {
	Name            string `json:"name"`
	Version         string `json:"version"`
	ProtocolVersion string `json:"protocolVersion"`
	Instructions    string `json:"instructions,omitempty"`
}

// MCPInspectCapabilities is a simplified view of what the server supports.
type MCPInspectCapabilities struct {
	Tools       bool `json:"tools"`
	Prompts     bool `json:"prompts"`
	Resources   bool `json:"resources"`
	Logging     bool `json:"logging"`
	Completions bool `json:"completions"`
}

// MCPInspectTool describes a tool discovered during inspect.
type MCPInspectTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"inputSchema,omitempty"`
}

// MCPInspectPrompt describes a prompt discovered during inspect.
type MCPInspectPrompt struct {
	Name        string                `json:"name"`
	Description string                `json:"description,omitempty"`
	Arguments   []MCPInspectPromptArg `json:"arguments,omitempty"`
}

// MCPInspectPromptArg describes a prompt argument.
type MCPInspectPromptArg struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required"`
}

// MCPInspectResource describes a resource discovered during inspect.
type MCPInspectResource struct {
	URI         string `json:"uri"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	MimeType    string `json:"mimeType,omitempty"`
}

// MCPInspectResourceTemplate describes a resource template discovered during
// inspect.
type MCPInspectResourceTemplate struct {
	URITemplate string `json:"uriTemplate"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	MimeType    string `json:"mimeType,omitempty"`
}

// SyncMCPServer re-discovers a server's tools (auto-discover mode: memory
// connects to the external server and calls tools/list). The response body is
// ignored; only the transport error matters.
func (m *MemoryClient) SyncMCPServer(ctx context.Context, id string) error {
	return m.do(ctx, http.MethodPost, "/api/admin/mcp-servers/"+id+"/sync", nil, nil)
}

// InspectMCPServer runs an ephemeral capability probe against the server.
// Memory always answers 200 — a connection failure comes back inside the
// result (Status "error", Error set), not as an HTTP error.
func (m *MemoryClient) InspectMCPServer(ctx context.Context, id string) (*MCPInspectResult, error) {
	var env successEnvelope[MCPInspectResult]
	if err := m.do(ctx, http.MethodPost, "/api/admin/mcp-servers/"+id+"/inspect", nil, &env); err != nil {
		return nil, err
	}
	return &env.Data, nil
}

// ListMCPServerTools lists the tools cached for one server. Memory's DTO
// carries extra fields (inputSchema, config, timestamps) beyond the subset
// MCPTool exposes; they are ignored.
func (m *MemoryClient) ListMCPServerTools(ctx context.Context, id string) ([]MCPTool, error) {
	var env successEnvelope[[]MCPTool]
	if err := m.do(ctx, http.MethodGet, "/api/admin/mcp-servers/"+id+"/tools", nil, &env); err != nil {
		return nil, err
	}
	return env.Data, nil
}

// SetMCPServerToolEnabled enables or disables one cached tool via a partial
// PATCH. The response body is ignored; only the transport error matters.
func (m *MemoryClient) SetMCPServerToolEnabled(ctx context.Context, id string, toolID string, enabled bool) error {
	return m.do(ctx, http.MethodPatch, "/api/admin/mcp-servers/"+id+"/tools/"+toolID, map[string]any{"enabled": enabled}, nil)
}

// BuiltinToolGroup mirrors one entry of memory's builtin tool capability
// taxonomy (GET /api/admin/builtin-tool-groups). It is the server-owned
// grouping of memory's native tools: frozen group order, tool membership as the
// same tool-name strings the flat MCPTool rows carry. Policy/Enabled are
// present for parity with the agent picker's ToolGroup but this view always
// reports the tools as enabled with no policy.
type BuiltinToolGroup struct {
	ID          string   `json:"id"`
	Label       string   `json:"label"`
	Description string   `json:"description,omitempty"`
	Policy      string   `json:"policy,omitempty"`
	Enabled     bool     `json:"enabled"`
	Tools       []string `json:"tools"`
}

// MCPToolCallContent is one content block in a tool-call result. It mirrors the
// MCP spec's content union; only the text variant is rendered specially, other
// types fall back to a label.
type MCPToolCallContent struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// MCPToolCallResult mirrors memory's tool-call response data (the `data` field
// of POST /api/admin/mcp-servers/:id/tools/:toolName/call). Content holds the
// MCP content blocks; IsError is true only when the call itself failed (an
// unknown tool or invalid invocation), not for a negative business result.
type MCPToolCallResult struct {
	Content           []MCPToolCallContent `json:"content"`
	IsError           bool                 `json:"isError"`
	StructuredContent json.RawMessage      `json:"structuredContent,omitempty"`
}

// ListBuiltinToolGroups fetches memory's builtin tool capability taxonomy: the
// frozen ordered groups (id/label/description) with their member tool names.
// The gateway only renders this grouping; it never derives it locally.
func (m *MemoryClient) ListBuiltinToolGroups(ctx context.Context) ([]BuiltinToolGroup, error) {
	var env successEnvelope[[]BuiltinToolGroup]
	if err := m.do(ctx, http.MethodGet, "/api/admin/builtin-tool-groups", nil, &env); err != nil {
		return nil, err
	}
	return env.Data, nil
}

// CallMCPServerTool invokes one tool on one server with the given JSON-object
// arguments (an empty/nil map runs the tool with no arguments) and returns the
// call result. Memory answers 200 for a completed call — including a negative
// business result and a call-level failure reported in IsError — so only a
// transport/HTTP failure surfaces as an error here.
func (m *MemoryClient) CallMCPServerTool(ctx context.Context, id, toolName string, args map[string]any) (*MCPToolCallResult, error) {
	if args == nil {
		args = map[string]any{}
	}
	path := "/api/admin/mcp-servers/" + id + "/tools/" + toolName + "/call"
	var env successEnvelope[MCPToolCallResult]
	if err := m.do(ctx, http.MethodPost, path, map[string]any{"arguments": args}, &env); err != nil {
		return nil, err
	}
	return &env.Data, nil
}
