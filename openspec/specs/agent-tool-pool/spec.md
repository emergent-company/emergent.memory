# agent-tool-pool Specification

## Purpose
TBD - created by archiving change multi-agent-coordination. Update Purpose after archive.
## Requirements
### Requirement: Project-Level Tool Pool

The system SHALL maintain a per-project ToolPool that combines built-in graph tools with external MCP server tools into a unified set.

#### Scenario: Tool pool initialization

- **WHEN** a project's ToolPool is first accessed
- **THEN** the system SHALL include all built-in graph tools from `domain/mcp/service.go` (30+ tools)
- **AND** discover tools from any configured external MCP servers via `tools/list`
- **AND** cache the combined tool set per project

#### Scenario: Tool pool cache invalidation

- **WHEN** a project's MCP server configuration changes (server added, removed, or updated)
- **THEN** the ToolPool cache for that project SHALL be invalidated
- **AND** the next tool resolution SHALL rebuild the combined tool set

### Requirement: Per-Agent Tool Filtering

The system SHALL filter the project's ToolPool to only the tools listed in an agent definition's `tools` whitelist.

#### Scenario: Exact tool name matching

- **WHEN** an agent definition has `tools: ["search_hybrid", "entities_get", "graph_traverse"]`
- **THEN** ResolveTools SHALL return only those 3 tools from the project's ToolPool
- **AND** no other tools SHALL be available to the agent's ADK pipeline

#### Scenario: Glob pattern matching

- **WHEN** an agent definition has `tools: ["entities_*"]`
- **THEN** ResolveTools SHALL return all tools whose names match the glob pattern (e.g., `entities_create`, `entities_get`, `entities_update`, `entities_delete`)

#### Scenario: Wildcard access

- **WHEN** an agent definition has `tools: ["*"]`
- **THEN** ResolveTools SHALL return all tools in the project's ToolPool

#### Scenario: Tool not found in pool

- **WHEN** an agent definition references a tool name that does not exist in the project's ToolPool
- **THEN** the system SHALL log a warning
- **AND** skip the unresolved tool (do not fail the entire resolution)

### Requirement: Sub-Agent Tool Restrictions

The system SHALL enforce system-level tool restrictions on sub-agents to prevent recursive spawning by default.

#### Scenario: Default sub-agent denied tools

- **WHEN** a sub-agent is spawned at depth > 0 without explicit opt-in to coordination tools
- **THEN** `spawn_agents` and `list_available_agents` SHALL be removed from the sub-agent's resolved tools
- **AND** this removal SHALL apply regardless of the agent definition's `tools` whitelist

#### Scenario: Opt-in to delegation

- **WHEN** a sub-agent's definition explicitly includes `spawn_agents` in its `tools` list AND the spawn depth is less than `max_depth` (default 2)
- **THEN** the sub-agent SHALL retain `spawn_agents` in its resolved tools
- **AND** the depth counter SHALL be incremented for any sub-sub-agents it spawns

#### Scenario: Max depth enforcement

- **WHEN** a sub-agent at depth >= `max_depth` (default 2) attempts to retain `spawn_agents`
- **THEN** `spawn_agents` SHALL be removed regardless of the agent definition
- **AND** a warning SHALL be logged indicating max depth was reached

### Requirement: Tool Filtering Security Enforcement

Tool filtering SHALL be enforced at the Go level, not by the LLM. The ADK pipeline SHALL only be built with resolved tools.

#### Scenario: LLM cannot access unresolved tools

- **WHEN** an agent's LLM generates a tool call for a tool not in its resolved set
- **THEN** the ADK pipeline SHALL not have that tool registered
- **AND** the tool call SHALL fail with a "tool not found" error returned to the LLM

#### Scenario: Glob resolution at build time

- **WHEN** glob patterns are resolved during pipeline construction
- **THEN** the resolution SHALL be fixed for the duration of that execution
- **AND** new tools matching the glob added mid-execution SHALL NOT be available until the next execution

