## MODIFIED Requirements

### Requirement: Built-in Tool Availability

The system SHALL conditionally include built-in tools based on the three-tier inheritance resolution (project → org → global env), not solely based on whether a global env var is set.

#### Scenario: brave_web_search enabled via project config

- **WHEN** `mcp_server_tools.config->>'api_key'` is set for a project's `brave_web_search` tool row
- **THEN** `brave_web_search` SHALL be included in that project's tool pool
- **AND** the tool SHALL execute using the project-level API key

#### Scenario: brave_web_search enabled via org config

- **WHEN** no project-level config exists but `core.org_tool_settings.config->>'api_key'` is set for the org
- **THEN** `brave_web_search` SHALL be included in the project's tool pool
- **AND** the tool SHALL execute using the org-level API key

#### Scenario: brave_web_search falls back to global env

- **WHEN** no project or org config exists
- **AND** `BRAVE_SEARCH_API_KEY` env var is non-empty
- **THEN** `brave_web_search` SHALL be included in all projects' tool pools as before
- **AND** the tool SHALL execute using the global API key

#### Scenario: brave_web_search disabled per project

- **WHEN** `mcp_server_tools.enabled = false` for a project's `brave_web_search` row
- **THEN** `brave_web_search` SHALL NOT be included in that project's tool pool
- **AND** this overrides any org or global setting
