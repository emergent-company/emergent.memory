# mcp-integration Delta Spec

## ADDED Requirements

### Requirement: get_project_info builtin tool registered
The builtin MCP tool registry SHALL include `get_project_info` in the list returned by `GetToolDefinitions()`, making it available to all projects via the standard `EnsureBuiltinServer` / `GetEnabledBuiltinToolsForProject` flow.

#### Scenario: get_project_info appears in tool definitions
- **WHEN** `mcp.Service.GetToolDefinitions()` is called
- **THEN** the returned slice includes a tool with `name: "get_project_info"`
- **AND** the tool has an empty `inputSchema` (no required parameters)

#### Scenario: get_project_info upserted into mcp_server_tools on first project access
- **WHEN** `EnsureBuiltinServer` runs for a project after a server restart
- **THEN** a row for `get_project_info` is upserted into `kb.mcp_server_tools` with `enabled = true`
