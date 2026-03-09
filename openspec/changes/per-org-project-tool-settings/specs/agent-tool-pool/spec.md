## MODIFIED Requirements

### Requirement: Project-Level Tool Pool

The system SHALL maintain a per-project ToolPool that combines built-in graph tools (filtered by per-project enabled state) with external MCP server tools into a unified set.

#### Scenario: Tool pool initialization

- **WHEN** a project's ToolPool is first accessed
- **THEN** the system SHALL call `EnsureBuiltinServer` to guarantee builtin tool rows exist in `kb.mcp_server_tools`
- **AND** include only built-in tools that are effectively enabled for the project (via three-tier inheritance resolution)
- **AND** discover tools from any configured external MCP servers via `tools/list`
- **AND** cache the combined tool set per project

#### Scenario: Disabled builtin tool excluded from pool

- **WHEN** a built-in tool's effective enabled state is `false` for a project
- **THEN** that tool SHALL NOT be included in the project's ToolPool
- **AND** it SHALL be silently omitted (no error thrown during pool build)

#### Scenario: Tool pool cache invalidation

- **WHEN** a project's MCP server configuration changes (server added, removed, or updated), OR a built-in tool setting is toggled at project or org level
- **THEN** the ToolPool cache for that project SHALL be invalidated
- **AND** the next tool resolution SHALL rebuild the combined tool set
