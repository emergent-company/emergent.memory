## ADDED Requirements

### Requirement: Admin can generate a read-only MCP API token
A project admin SHALL be able to generate a read-only API token scoped to a specific project via `POST /api/projects/{projectId}/mcp/share`. The token MUST be created with the `project_viewer` scope set (`data:read`, `schema:read`, `agents:read`, `projects:read`) and MUST NOT include any write scopes. The response MUST include the raw token value (returned only once), the MCP endpoint URL, the project ID, and pre-formatted agent config snippets.

#### Scenario: Successful token generation
- **WHEN** an admin sends `POST /api/projects/{projectId}/mcp/share` with a valid session
- **THEN** the system creates an API token with scopes `["data:read", "schema:read", "agents:read", "projects:read"]`
- **THEN** the response includes `token`, `mcpUrl`, `projectId`, and `snippets`
- **THEN** the raw token value is present in the response body exactly once and never returned again

#### Scenario: Non-admin cannot generate a share token
- **WHEN** a user without project admin privileges sends `POST /api/projects/{projectId}/mcp/share`
- **THEN** the system returns HTTP 403 Forbidden

#### Scenario: Token appears in existing token list
- **WHEN** an admin generates a read-only MCP share token
- **THEN** the token appears in `GET /api/projects/{projectId}/tokens` with its name and scopes
- **THEN** the token can be revoked via `DELETE /api/projects/{projectId}/tokens/{tokenId}`

### Requirement: Generated token has a descriptive auto-generated name
If no `name` is provided in the request body, the system SHALL assign a default name in the format `"MCP Read-Only Share — <YYYY-MM-DD>"`. If a `name` is provided, the system SHALL use that value instead.

#### Scenario: Default name assigned when omitted
- **WHEN** admin sends `POST /api/projects/{projectId}/mcp/share` without a `name` field
- **THEN** the created token has a name matching `"MCP Read-Only Share — <current date>"`

#### Scenario: Custom name used when provided
- **WHEN** admin sends `POST /api/projects/{projectId}/mcp/share` with `"name": "CI Agent Token"`
- **THEN** the created token has the name `"CI Agent Token"`

### Requirement: Token generation is idempotent-safe
The system SHALL allow multiple read-only MCP tokens to be generated for the same project. Each call to `POST /api/projects/{projectId}/mcp/share` MUST create a new, distinct token.

#### Scenario: Multiple tokens can coexist
- **WHEN** an admin calls `POST /api/projects/{projectId}/mcp/share` twice
- **THEN** two separate tokens exist in the token list, each with a unique ID and token value
