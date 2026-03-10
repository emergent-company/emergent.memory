# org-tool-settings Specification

## Purpose
Org-level default tool settings that allow org admins to configure which built-in MCP tools are enabled by default and provide default config values (e.g., API keys) for all projects in the org.

## Requirements

### Requirement: Org Tool Settings Storage

The system SHALL store per-org, per-tool settings in `core.org_tool_settings` with fields: `id`, `org_id`, `tool_name`, `enabled`, `config jsonb`, `created_at`, `updated_at`.

#### Scenario: Table creation

- **WHEN** the database migration `00045_create_org_tool_settings.sql` runs
- **THEN** the table `core.org_tool_settings` SHALL exist with columns `id uuid PK`, `org_id uuid NOT NULL REFERENCES core.orgs(id) ON DELETE CASCADE`, `tool_name text NOT NULL`, `enabled bool NOT NULL DEFAULT true`, `config jsonb`, `created_at timestamptz`, `updated_at timestamptz`
- **AND** a UNIQUE constraint SHALL exist on `(org_id, tool_name)`

### Requirement: List Org Tool Settings API

The system SHALL expose a `GET /api/admin/orgs/:orgId/tool-settings` endpoint that returns all tool settings configured for an org.

#### Scenario: Org admin retrieves tool settings

- **WHEN** an org admin calls `GET /api/admin/orgs/:orgId/tool-settings`
- **THEN** the response SHALL return HTTP 200 with a JSON array of org tool setting objects
- **AND** each object SHALL include `toolName`, `enabled`, `config`, `createdAt`, `updatedAt`
- **AND** only tools that have an explicit org setting SHALL be returned (not all possible tools)

#### Scenario: Non-member cannot access org tool settings

- **WHEN** a user who is not a member of the org calls `GET /api/admin/orgs/:orgId/tool-settings`
- **THEN** the response SHALL return HTTP 403

### Requirement: Upsert Org Tool Setting API

The system SHALL expose a `PUT /api/admin/orgs/:orgId/tool-settings/:toolName` endpoint that creates or updates a tool setting for an org.

#### Scenario: Org admin sets a tool default

- **WHEN** an org admin calls `PUT /api/admin/orgs/:orgId/tool-settings/brave_web_search` with body `{"enabled": true, "config": {"api_key": "sk-xxx"}}`
- **THEN** the response SHALL return HTTP 200 with the updated org tool setting
- **AND** the setting SHALL be persisted in `core.org_tool_settings`

#### Scenario: Upsert overwrites existing setting

- **WHEN** an org admin calls PUT with a `toolName` that already has an entry
- **THEN** the existing row SHALL be updated (enabled + config overwritten)
- **AND** `updated_at` SHALL be refreshed

#### Scenario: Non-org-admin cannot upsert tool settings

- **WHEN** a user without org admin role calls the PUT endpoint
- **THEN** the response SHALL return HTTP 403

### Requirement: Delete Org Tool Setting API

The system SHALL expose a `DELETE /api/admin/orgs/:orgId/tool-settings/:toolName` endpoint to remove an org-level override and revert to global defaults.

#### Scenario: Org admin removes a tool setting

- **WHEN** an org admin calls `DELETE /api/admin/orgs/:orgId/tool-settings/brave_web_search`
- **THEN** the response SHALL return HTTP 204
- **AND** the row SHALL be removed from `core.org_tool_settings`
- **AND** subsequent tool resolution for projects in this org SHALL fall back to the global env var default
