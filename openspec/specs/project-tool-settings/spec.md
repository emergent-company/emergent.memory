# project-tool-settings Specification

## Purpose
Per-project built-in tool settings that allow project admins to enable/disable individual built-in MCP tools and supply project-specific configuration (e.g., a dedicated API key for `brave_web_search`), always taking precedence over org defaults.

## Requirements

### Requirement: Project Tool Config Storage

The system SHALL store per-project, per-tool configuration in the existing `kb.mcp_server_tools.config` JSONB column.

#### Scenario: Column addition migration

- **WHEN** the database migration `00044_add_mcp_server_tool_config.sql` runs
- **THEN** the column `config jsonb` SHALL exist on `kb.mcp_server_tools`
- **AND** existing rows SHALL have `config = NULL`

### Requirement: Get Project Builtin Tool Settings API

The system SHALL return per-project built-in tool settings (including enabled flag, config, and inheritance source) via the existing `GET /api/admin/mcp-servers/:id/tools` endpoint when the server is of type `builtin`.

#### Scenario: Project admin views builtin tool settings

- **WHEN** a project admin calls `GET /api/admin/mcp-servers/:id/tools` for the project's builtin server
- **THEN** the response SHALL return HTTP 200 with an array of tools
- **AND** each tool SHALL include `toolName`, `enabled`, `config`, `inheritedFrom` (one of `"project"`, `"org"`, `"global"`)
- **AND** `inheritedFrom` SHALL be `"project"` when the project has an explicit setting
- **AND** `inheritedFrom` SHALL be `"org"` when the effective value comes from org default
- **AND** `inheritedFrom` SHALL be `"global"` when the effective value comes from the global env var

### Requirement: Update Project Tool Setting API

The system SHALL allow updating a project-level built-in tool's enabled state and config via the existing `PATCH /api/admin/mcp-servers/:id/tools/:toolId` endpoint, extended to accept a `config` field.

#### Scenario: Project admin enables a tool with custom API key

- **WHEN** a project admin calls `PATCH /api/admin/mcp-servers/:id/tools/:toolId` with body `{"enabled": true, "config": {"api_key": "sk-proj-xxx"}}`
- **THEN** the response SHALL return HTTP 200 with the updated tool setting
- **AND** both `enabled` and `config` SHALL be persisted to `kb.mcp_server_tools`
- **AND** subsequent tool resolution for this project SHALL use `source = "project"`

#### Scenario: Project admin disables a tool

- **WHEN** a project admin calls `PATCH` with `{"enabled": false}`
- **THEN** the tool SHALL be excluded from the project's agent tool pool
- **AND** the tool pool cache for this project SHALL be invalidated

#### Scenario: Project admin clears a project-level config to revert to org/global default

- **WHEN** a project admin calls `PATCH` with `{"config": null}`
- **THEN** `mcp_server_tools.config` SHALL be set to NULL for that tool
- **AND** subsequent resolution SHALL fall back to org or global default for config values

### Requirement: Builtin Tool Settings Visible in UI

The system SHALL display built-in tools in the project MCP settings page with their effective enabled state, config fields, and inheritance source indicator.

#### Scenario: Inherited value indicator shown

- **WHEN** a tool's effective setting comes from the org default (not a project override)
- **THEN** the UI SHALL display "Inherited from org" next to the tool's setting
- **AND** the toggle and config fields SHALL still be editable to create a project-level override

#### Scenario: Configurable tool shows config fields

- **WHEN** a tool has known configurable fields (e.g., `brave_web_search` has `api_key`)
- **THEN** the UI SHALL render an input for each config field
- **AND** the placeholder SHALL show the effective inherited value if one exists (masked for sensitive fields)
