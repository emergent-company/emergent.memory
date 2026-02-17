package mcpregistry

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/emergent-company/emergent/domain/mcp"
)

// MCPRegistryToolHandler implements mcp.MCPRegistryToolHandler, providing MCP
// registry management tools. It bridges the mcp package (which cannot import
// mcpregistry) to the mcpregistry domain by implementing the interface defined
// in mcp/entity.go.
type MCPRegistryToolHandler struct {
	service *Service
	log     *slog.Logger
}

// NewMCPRegistryToolHandler creates a new MCPRegistryToolHandler.
func NewMCPRegistryToolHandler(service *Service, log *slog.Logger) *MCPRegistryToolHandler {
	return &MCPRegistryToolHandler{
		service: service,
		log:     log,
	}
}

// wrapResult marshals data as indented JSON into an MCP ToolResult.
func wrapResult(data any) (*mcp.ToolResult, error) {
	jsonBytes, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal result: %w", err)
	}

	return &mcp.ToolResult{
		Content: []mcp.ContentBlock{
			{
				Type: "text",
				Text: string(jsonBytes),
			},
		},
	}, nil
}

// errResult creates an error ToolResult (non-fatal tool error returned to LLM).
func errResult(msg string) (*mcp.ToolResult, error) {
	return &mcp.ToolResult{
		Content: []mcp.ContentBlock{
			{Type: "text", Text: fmt.Sprintf(`{"error": %q}`, msg)},
		},
	}, nil
}

// ============================================================================
// MCP Server Tools
// ============================================================================

// ExecuteListMCPServers lists all MCP servers for a project.
func (h *MCPRegistryToolHandler) ExecuteListMCPServers(ctx context.Context, projectID string, args map[string]any) (*mcp.ToolResult, error) {
	// Ensure builtin server is registered first
	if err := h.service.EnsureBuiltinServer(ctx, projectID); err != nil {
		h.log.Warn("failed to ensure builtin server", slog.String("error", err.Error()))
	}

	servers, err := h.service.ListServers(ctx, projectID)
	if err != nil {
		return errResult("failed to list MCP servers: " + err.Error())
	}

	dtos := make([]*MCPServerDTO, len(servers))
	for i, s := range servers {
		dtos[i] = s.ToDTO()
	}

	return wrapResult(dtos)
}

// ExecuteGetMCPServer gets a single MCP server by ID with its tools.
func (h *MCPRegistryToolHandler) ExecuteGetMCPServer(ctx context.Context, projectID string, args map[string]any) (*mcp.ToolResult, error) {
	id, _ := args["server_id"].(string)
	if id == "" {
		return errResult("server_id is required")
	}

	server, err := h.service.GetServer(ctx, id, &projectID)
	if err != nil {
		return errResult("failed to get MCP server: " + err.Error())
	}
	if server == nil {
		return errResult(fmt.Sprintf("MCP server not found: %s", id))
	}

	return wrapResult(server.ToDetailDTO())
}

// ExecuteCreateMCPServer creates a new external MCP server.
func (h *MCPRegistryToolHandler) ExecuteCreateMCPServer(ctx context.Context, projectID string, args map[string]any) (*mcp.ToolResult, error) {
	name, _ := args["name"].(string)
	if name == "" {
		return errResult("name is required")
	}

	serverType, _ := args["type"].(string)
	if serverType == "" {
		return errResult("type is required (stdio, sse, or http)")
	}

	dto := &CreateMCPServerDTO{
		Name: name,
		Type: MCPServerType(serverType),
	}

	// Optional fields
	if cmd, ok := args["command"].(string); ok {
		dto.Command = &cmd
	}
	if url, ok := args["url"].(string); ok {
		dto.URL = &url
	}
	if enabled, ok := args["enabled"].(bool); ok {
		dto.Enabled = &enabled
	}
	if argsArr, ok := args["args"].([]any); ok {
		for _, v := range argsArr {
			if s, ok := v.(string); ok {
				dto.Args = append(dto.Args, s)
			}
		}
	}
	if env, ok := args["env"].(map[string]any); ok {
		dto.Env = env
	}
	if headers, ok := args["headers"].(map[string]any); ok {
		dto.Headers = headers
	}

	server, err := h.service.CreateServer(ctx, projectID, dto)
	if err != nil {
		return errResult("failed to create MCP server: " + err.Error())
	}

	return wrapResult(server.ToDTO())
}

// ExecuteUpdateMCPServer updates an existing MCP server (partial update).
func (h *MCPRegistryToolHandler) ExecuteUpdateMCPServer(ctx context.Context, projectID string, args map[string]any) (*mcp.ToolResult, error) {
	id, _ := args["server_id"].(string)
	if id == "" {
		return errResult("server_id is required")
	}

	dto := &UpdateMCPServerDTO{}

	if name, ok := args["name"].(string); ok {
		dto.Name = &name
	}
	if enabled, ok := args["enabled"].(bool); ok {
		dto.Enabled = &enabled
	}
	if cmd, ok := args["command"].(string); ok {
		dto.Command = &cmd
	}
	if url, ok := args["url"].(string); ok {
		dto.URL = &url
	}
	if argsArr, ok := args["args"].([]any); ok {
		strArgs := make([]string, 0, len(argsArr))
		for _, v := range argsArr {
			if s, ok := v.(string); ok {
				strArgs = append(strArgs, s)
			}
		}
		dto.Args = strArgs
	}
	if env, ok := args["env"].(map[string]any); ok {
		dto.Env = env
	}
	if headers, ok := args["headers"].(map[string]any); ok {
		dto.Headers = headers
	}

	server, err := h.service.UpdateServer(ctx, id, projectID, dto)
	if err != nil {
		return errResult("failed to update MCP server: " + err.Error())
	}
	if server == nil {
		return errResult(fmt.Sprintf("MCP server not found: %s", id))
	}

	return wrapResult(server.ToDTO())
}

// ExecuteDeleteMCPServer deletes an MCP server by ID.
func (h *MCPRegistryToolHandler) ExecuteDeleteMCPServer(ctx context.Context, projectID string, args map[string]any) (*mcp.ToolResult, error) {
	id, _ := args["server_id"].(string)
	if id == "" {
		return errResult("server_id is required")
	}

	if err := h.service.DeleteServer(ctx, id, projectID); err != nil {
		return errResult("failed to delete MCP server: " + err.Error())
	}

	return wrapResult(map[string]any{
		"success": true,
		"message": fmt.Sprintf("MCP server %s deleted", id),
	})
}

// ExecuteToggleMCPServerTool enables or disables a specific tool.
func (h *MCPRegistryToolHandler) ExecuteToggleMCPServerTool(ctx context.Context, projectID string, args map[string]any) (*mcp.ToolResult, error) {
	toolID, _ := args["tool_id"].(string)
	if toolID == "" {
		return errResult("tool_id is required")
	}

	enabled, ok := args["enabled"].(bool)
	if !ok {
		return errResult("enabled is required (boolean)")
	}

	if err := h.service.ToggleTool(ctx, toolID, enabled); err != nil {
		return errResult("failed to toggle tool: " + err.Error())
	}

	return wrapResult(map[string]any{
		"success": true,
		"tool_id": toolID,
		"enabled": enabled,
		"message": fmt.Sprintf("Tool %s %s", toolID, map[bool]string{true: "enabled", false: "disabled"}[enabled]),
	})
}

// ExecuteSyncMCPServerTools syncs tools from an external MCP server.
// If no "tools" argument is provided, it auto-discovers by connecting to the
// server and calling tools/list. Otherwise falls back to manual sync with
// the provided tool definitions.
func (h *MCPRegistryToolHandler) ExecuteSyncMCPServerTools(ctx context.Context, projectID string, args map[string]any) (*mcp.ToolResult, error) {
	serverID, _ := args["server_id"].(string)
	if serverID == "" {
		return errResult("server_id is required")
	}

	// Check if tools are explicitly provided (manual sync mode)
	var discoveredTools []DiscoveredTool
	if toolsArg, ok := args["tools"].([]any); ok && len(toolsArg) > 0 {
		for _, t := range toolsArg {
			if toolMap, ok := t.(map[string]any); ok {
				dt := DiscoveredTool{}
				if name, ok := toolMap["name"].(string); ok {
					dt.Name = name
				}
				if desc, ok := toolMap["description"].(string); ok {
					dt.Description = &desc
				}
				if schema, ok := toolMap["inputSchema"].(map[string]any); ok {
					dt.InputSchema = schema
				}
				if dt.Name != "" {
					discoveredTools = append(discoveredTools, dt)
				}
			}
		}
	}

	var toolCount int

	if len(discoveredTools) > 0 {
		// Manual sync with provided tools
		if err := h.service.SyncServerTools(ctx, serverID, discoveredTools); err != nil {
			return errResult("failed to sync tools: " + err.Error())
		}
		toolCount = len(discoveredTools)
	} else {
		// Auto-discover: connect to server and call tools/list
		discovered, err := h.service.DiscoverAndSyncTools(ctx, serverID, projectID)
		if err != nil {
			return errResult("failed to discover and sync tools: " + err.Error())
		}
		toolCount = len(discovered)
	}

	return wrapResult(map[string]any{
		"success":    true,
		"server_id":  serverID,
		"tool_count": toolCount,
		"message":    fmt.Sprintf("Synced %d tools for server %s", toolCount, serverID),
	})
}

// ============================================================================
// Official MCP Registry Tools
// ============================================================================

// ExecuteSearchMCPRegistry searches the official MCP registry for servers.
func (h *MCPRegistryToolHandler) ExecuteSearchMCPRegistry(ctx context.Context, projectID string, args map[string]any) (*mcp.ToolResult, error) {
	query, _ := args["query"].(string)

	limit := 20
	if l, ok := args["limit"].(float64); ok {
		limit = int(l)
	}

	cursor, _ := args["cursor"].(string)

	result, err := h.service.SearchRegistry(ctx, query, limit, cursor)
	if err != nil {
		return errResult("failed to search MCP registry: " + err.Error())
	}

	return wrapResult(result)
}

// ExecuteGetMCPRegistryServer gets details of a specific server from the official MCP registry.
func (h *MCPRegistryToolHandler) ExecuteGetMCPRegistryServer(ctx context.Context, projectID string, args map[string]any) (*mcp.ToolResult, error) {
	name, _ := args["name"].(string)
	if name == "" {
		return errResult("name is required (e.g. 'io.github.github/github-mcp-server')")
	}

	result, err := h.service.GetRegistryServer(ctx, name)
	if err != nil {
		return errResult("failed to get registry server: " + err.Error())
	}

	return wrapResult(result)
}

// ExecuteInstallMCPFromRegistry installs a server from the official MCP registry into the current project.
func (h *MCPRegistryToolHandler) ExecuteInstallMCPFromRegistry(ctx context.Context, projectID string, args map[string]any) (*mcp.ToolResult, error) {
	registryName, _ := args["registry_name"].(string)
	if registryName == "" {
		return errResult("registry_name is required (e.g. 'io.github.github/github-mcp-server')")
	}

	dto := &InstallFromRegistryDTO{
		RegistryName: registryName,
	}

	if version, ok := args["version"].(string); ok && version != "" {
		dto.Version = version
	}
	if name, ok := args["name"].(string); ok && name != "" {
		dto.Name = name
	}

	result, err := h.service.InstallFromRegistry(ctx, projectID, dto)
	if err != nil {
		return errResult("failed to install from registry: " + err.Error())
	}

	return wrapResult(result)
}

// ============================================================================
// Inspect Tool
// ============================================================================

// ExecuteInspectMCPServer inspects/test-connects an MCP server by ID.
func (h *MCPRegistryToolHandler) ExecuteInspectMCPServer(ctx context.Context, projectID string, args map[string]any) (*mcp.ToolResult, error) {
	serverID, _ := args["server_id"].(string)
	if serverID == "" {
		return errResult("server_id is required")
	}

	result, err := h.service.InspectServer(ctx, serverID, projectID)
	if err != nil {
		return errResult("failed to inspect MCP server: " + err.Error())
	}
	if result == nil {
		return errResult(fmt.Sprintf("MCP server not found: %s", serverID))
	}

	return wrapResult(result)
}

// ============================================================================
// Tool Definitions
// ============================================================================

// GetMCPRegistryToolDefinitions returns MCP tool definitions for all registry tools.
func (h *MCPRegistryToolHandler) GetMCPRegistryToolDefinitions() []mcp.ToolDefinition {
	return []mcp.ToolDefinition{
		{
			Name:        "list_mcp_servers",
			Description: "List all registered MCP servers for the current project. Returns both builtin and external servers with their type, status, and tool count.",
			InputSchema: mcp.InputSchema{
				Type:       "object",
				Properties: map[string]mcp.PropertySchema{},
				Required:   []string{},
			},
		},
		{
			Name:        "get_mcp_server",
			Description: "Get details of a specific MCP server by ID, including all its registered tools with their enabled/disabled status.",
			InputSchema: mcp.InputSchema{
				Type: "object",
				Properties: map[string]mcp.PropertySchema{
					"server_id": {
						Type:        "string",
						Description: "The UUID of the MCP server to retrieve",
					},
				},
				Required: []string{"server_id"},
			},
		},
		{
			Name:        "create_mcp_server",
			Description: "Register a new external MCP server. Supports stdio, sse, and http transport types. Builtin servers are managed automatically.",
			InputSchema: mcp.InputSchema{
				Type: "object",
				Properties: map[string]mcp.PropertySchema{
					"name": {
						Type:        "string",
						Description: "Unique name for the MCP server within this project",
					},
					"type": {
						Type:        "string",
						Description: "Transport type for connecting to the server",
						Enum:        []string{"stdio", "sse", "http"},
					},
					"command": {
						Type:        "string",
						Description: "Command to execute (required for stdio type)",
					},
					"args": {
						Type:        "string",
						Description: "JSON array of command arguments (for stdio type)",
					},
					"url": {
						Type:        "string",
						Description: "Server URL (required for sse and http types)",
					},
					"env": {
						Type:        "string",
						Description: "JSON object of environment variables (for stdio type)",
					},
					"headers": {
						Type:        "string",
						Description: "JSON object of HTTP headers (for sse and http types)",
					},
					"enabled": {
						Type:        "boolean",
						Description: "Whether the server is enabled (default: true)",
						Default:     true,
					},
				},
				Required: []string{"name", "type"},
			},
		},
		{
			Name:        "update_mcp_server",
			Description: "Update an existing MCP server configuration. Only provided fields are updated (partial update). Builtin servers can only be enabled/disabled.",
			InputSchema: mcp.InputSchema{
				Type: "object",
				Properties: map[string]mcp.PropertySchema{
					"server_id": {
						Type:        "string",
						Description: "The UUID of the MCP server to update",
					},
					"name": {
						Type:        "string",
						Description: "New name for the server",
					},
					"enabled": {
						Type:        "boolean",
						Description: "Enable or disable the server",
					},
					"command": {
						Type:        "string",
						Description: "New command (stdio type only)",
					},
					"url": {
						Type:        "string",
						Description: "New URL (sse/http types only)",
					},
				},
				Required: []string{"server_id"},
			},
		},
		{
			Name:        "delete_mcp_server",
			Description: "Delete an external MCP server and all its cached tools. Builtin servers cannot be deleted.",
			InputSchema: mcp.InputSchema{
				Type: "object",
				Properties: map[string]mcp.PropertySchema{
					"server_id": {
						Type:        "string",
						Description: "The UUID of the MCP server to delete",
					},
				},
				Required: []string{"server_id"},
			},
		},
		{
			Name:        "toggle_mcp_server_tool",
			Description: "Enable or disable a specific tool from an MCP server. Disabled tools are excluded from the agent tool pool.",
			InputSchema: mcp.InputSchema{
				Type: "object",
				Properties: map[string]mcp.PropertySchema{
					"tool_id": {
						Type:        "string",
						Description: "The UUID of the tool to toggle",
					},
					"enabled": {
						Type:        "boolean",
						Description: "Whether the tool should be enabled",
					},
				},
				Required: []string{"tool_id", "enabled"},
			},
		},
		{
			Name:        "sync_mcp_server_tools",
			Description: "Sync tool definitions from an external MCP server. By default, connects to the server and calls tools/list to auto-discover available tools. Optionally, tools can be provided manually. New tools are enabled by default; removed tools are cleaned up.",
			InputSchema: mcp.InputSchema{
				Type: "object",
				Properties: map[string]mcp.PropertySchema{
					"server_id": {
						Type:        "string",
						Description: "The UUID of the MCP server to sync tools from",
					},
					"tools": {
						Type:        "string",
						Description: "Optional JSON array of tool definitions with name, description, and inputSchema. If omitted, tools are auto-discovered by connecting to the server.",
					},
				},
				Required: []string{"server_id"},
			},
		},
		{
			Name:        "search_mcp_registry",
			Description: "Search the official MCP registry (registry.modelcontextprotocol.io) for available MCP servers. Returns server names, descriptions, available transports (remote/stdio), and required environment variables.",
			InputSchema: mcp.InputSchema{
				Type: "object",
				Properties: map[string]mcp.PropertySchema{
					"query": {
						Type:        "string",
						Description: "Search query for finding MCP servers (case-insensitive substring match on server names). Leave empty to list all servers.",
					},
					"limit": {
						Type:        "number",
						Description: "Maximum number of results to return (default: 20, max: 100)",
						Default:     float64(20),
					},
					"cursor": {
						Type:        "string",
						Description: "Pagination cursor from a previous search result's nextCursor field",
					},
				},
				Required: []string{},
			},
		},
		{
			Name:        "get_mcp_registry_server",
			Description: "Get detailed information about a specific server from the official MCP registry, including all available transports (remotes, packages), required environment variables, and repository links.",
			InputSchema: mcp.InputSchema{
				Type: "object",
				Properties: map[string]mcp.PropertySchema{
					"name": {
						Type:        "string",
						Description: "The registry server name (e.g. 'io.github.github/github-mcp-server'). Use search_mcp_registry to find server names.",
					},
				},
				Required: []string{"name"},
			},
		},
		{
			Name:        "install_mcp_from_registry",
			Description: "Install an MCP server from the official registry into the current project. Only servers with remote transports (HTTP/SSE) are supported — stdio-based packages (npm/pypi/oci) are blocked for security. Creates the server entry, attempts tool discovery, and returns required environment variables that must be configured before use.",
			InputSchema: mcp.InputSchema{
				Type: "object",
				Properties: map[string]mcp.PropertySchema{
					"registry_name": {
						Type:        "string",
						Description: "The registry server name to install (e.g. 'io.github.github/github-mcp-server'). Use search_mcp_registry to find available servers.",
					},
					"version": {
						Type:        "string",
						Description: "Version to install (default: 'latest')",
					},
					"name": {
						Type:        "string",
						Description: "Optional custom name for the installed server. Defaults to a name derived from the registry name (e.g. 'github-mcp-server').",
					},
				},
				Required: []string{"registry_name"},
			},
		},
		{
			Name:        "inspect_mcp_server",
			Description: "Inspect/test-connect an MCP server. Creates a fresh ephemeral connection, captures the server's identity (name, version, protocol), capabilities, and enumerates all tools, prompts, and resources the server exposes. Connection errors are reported in the response (status: 'error') rather than as failures.",
			InputSchema: mcp.InputSchema{
				Type: "object",
				Properties: map[string]mcp.PropertySchema{
					"server_id": {
						Type:        "string",
						Description: "The UUID of the MCP server to inspect",
					},
				},
				Required: []string{"server_id"},
			},
		},
	}
}
