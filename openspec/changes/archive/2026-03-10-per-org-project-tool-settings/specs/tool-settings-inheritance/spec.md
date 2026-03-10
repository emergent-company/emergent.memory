# tool-settings-inheritance Specification

## Purpose
Three-tier inheritance logic that resolves the effective enabled state and configuration for any built-in tool by checking project settings first, then org defaults, then global env vars.

## Requirements

### Requirement: Effective Tool Settings Resolution

The system SHALL resolve the effective enabled state and configuration for a built-in tool using a three-tier inheritance chain: project setting → org default → global env var.

#### Scenario: Project setting takes precedence

- **WHEN** a project has an explicit entry in `kb.mcp_server_tools` for a tool (enabled or disabled)
- **THEN** that setting SHALL be used as the effective value regardless of org or global settings
- **AND** the resolution source SHALL be reported as `"project"`

#### Scenario: Org default used when no project override

- **WHEN** a project has no explicit entry in `kb.mcp_server_tools` for a tool
- **AND** the project's org has a row in `core.org_tool_settings` for that tool
- **THEN** the org setting SHALL be used as the effective value
- **AND** the resolution source SHALL be reported as `"org"`

#### Scenario: Global env var used as final fallback

- **WHEN** no project-level and no org-level setting exists for a tool
- **THEN** the system SHALL fall back to the global env var (e.g., `BRAVE_SEARCH_API_KEY` non-empty → enabled)
- **AND** the resolution source SHALL be reported as `"global"`

#### Scenario: Tool disabled with no config at any level

- **WHEN** no setting exists at any level and the global env var is not set
- **THEN** the tool SHALL be treated as disabled
- **AND** SHALL NOT appear in the agent tool pool

### Requirement: Config Inheritance

Config values (e.g., API keys) SHALL be resolved independently using the same three-tier chain.

#### Scenario: Project-level API key overrides org and global

- **WHEN** `mcp_server_tools.config->>'api_key'` is set for a project's tool row
- **THEN** that key SHALL be used for tool execution
- **AND** org and global keys SHALL be ignored

#### Scenario: Org-level API key used when no project key

- **WHEN** no project-level `api_key` is set
- **AND** `core.org_tool_settings.config->>'api_key'` is set for the org
- **THEN** the org key SHALL be used for tool execution

#### Scenario: Global env var key used as final fallback

- **WHEN** no project or org API key is set
- **THEN** the global env var value SHALL be used
- **AND** if no global key exists either, tool execution SHALL return an error indicating missing configuration

### Requirement: Tool Pool Respects Effective Enabled State

The agent tool pool SHALL only include built-in tools whose effective enabled state resolves to `true`.

#### Scenario: Disabled tool absent from pool

- **WHEN** a tool's effective enabled state is `false` (at any tier)
- **THEN** the tool SHALL NOT be included in the project's agent tool pool
- **AND** agents with that tool in their whitelist SHALL receive a "tool not found" warning (not a hard error)

#### Scenario: Tool pool cache invalidated on setting change

- **WHEN** a project or org tool setting is updated
- **THEN** the tool pool cache for affected projects SHALL be invalidated
- **AND** the next agent execution SHALL rebuild the tool pool with updated settings

### Requirement: E2E Tool Execution with Project-Level Config

The system SHALL correctly execute a built-in tool using a project-level API key configured via the tool settings API.

#### Scenario: brave_web_search executes with project API key

- **WHEN** a project has `brave_web_search` enabled with `config.api_key = "<valid-key>"` in `mcp_server_tools`
- **AND** an agent with `brave_web_search` in its tools whitelist is triggered
- **THEN** the agent SHALL execute `brave_web_search` using the project-level API key
- **AND** the tool call SHALL be recorded in `kb.agent_run_tool_calls`
- **AND** the response SHALL contain web search results
