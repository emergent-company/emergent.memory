## ADDED Requirements

### Requirement: ACP ping endpoint
The system SHALL expose `GET /acp/v1/ping` that returns HTTP 200 with an empty JSON body `{}`. This endpoint SHALL NOT require authentication and SHALL be used for health checks by ACP clients.

#### Scenario: Ping returns 200
- **WHEN** a client sends `GET /acp/v1/ping` with no auth header
- **THEN** the server responds with HTTP 200 and body `{}`

### Requirement: List externally-visible agents
The system SHALL expose `GET /acp/v1/agents` that returns a JSON array of ACP agent manifests for all `AgentDefinition` records where `visibility = 'external'` within the authenticated user's project.

#### Scenario: List agents returns external agents only
- **WHEN** an authenticated client sends `GET /acp/v1/agents` with a valid `Bearer emt_*` token having `agents:read` scope
- **THEN** the server responds with HTTP 200 and a JSON array containing only agent definitions with `visibility = 'external'`

#### Scenario: List agents excludes project and internal agents
- **WHEN** a project has 3 agent definitions: one `external`, one `project`, one `internal`
- **THEN** `GET /acp/v1/agents` returns exactly 1 agent manifest (the `external` one)

#### Scenario: List agents with no auth returns 401
- **WHEN** a client sends `GET /acp/v1/agents` without an auth header
- **THEN** the server responds with HTTP 401

#### Scenario: List agents with insufficient scope returns 403
- **WHEN** a client sends `GET /acp/v1/agents` with a token that lacks `agents:read` scope
- **THEN** the server responds with HTTP 403

### Requirement: Get agent manifest by name
The system SHALL expose `GET /acp/v1/agents/:name` that returns a single ACP agent manifest by its slug-normalized name. The `:name` parameter SHALL match the `ACPSlug` derived from the agent definition's name.

#### Scenario: Get agent by slug name
- **WHEN** an authenticated client sends `GET /acp/v1/agents/my-cool-agent` and a definition with slug `my-cool-agent` exists with `visibility = 'external'`
- **THEN** the server responds with HTTP 200 and the full agent manifest JSON

#### Scenario: Get non-existent agent returns 404
- **WHEN** an authenticated client sends `GET /acp/v1/agents/nonexistent`
- **THEN** the server responds with HTTP 404

#### Scenario: Get non-external agent returns 404
- **WHEN** an authenticated client sends `GET /acp/v1/agents/internal-agent` and the definition exists but has `visibility = 'project'`
- **THEN** the server responds with HTTP 404 (non-external agents are not discoverable via ACP)

### Requirement: Agent manifest format
Each agent manifest returned by discovery endpoints SHALL include the following fields: `name` (RFC 1123 DNS label slug), `description`, `provider` (object with `organization` and `url`), `version`, `capabilities` (object), `default_input_modes` (array of MIME types), `default_output_modes` (array of MIME types). The manifest SHALL also include optional metadata fields when present in `ACPConfig`: `tags`, `domains`, `recommended_models`, `documentation`, `framework`, `links`, `dependencies`.

#### Scenario: Manifest contains required fields
- **WHEN** an agent with `visibility = 'external'` and populated `ACPConfig` is fetched via `GET /acp/v1/agents/:name`
- **THEN** the response body contains `name`, `description`, `provider`, `version`, `capabilities`, `default_input_modes`, `default_output_modes`

#### Scenario: Manifest omits empty optional fields
- **WHEN** an agent has no `tags`, `domains`, or `recommended_models` in its ACPConfig
- **THEN** the response body omits those fields (not present as empty arrays)

### Requirement: Agent slug normalization
Agent names exposed via ACP SHALL be derived from `AgentDefinition.Name` using RFC 1123 DNS label normalization: lowercase, replace non-alphanumeric characters with hyphens, collapse consecutive hyphens, trim leading/trailing hyphens, truncate to 63 characters.

#### Scenario: Name with spaces and mixed case
- **WHEN** an agent definition has name `My Cool Agent`
- **THEN** its ACP slug is `my-cool-agent`

#### Scenario: Name with special characters
- **WHEN** an agent definition has name `Agent (v2.1) — Production`
- **THEN** its ACP slug is `agent-v2-1-production`

#### Scenario: Very long name is truncated
- **WHEN** an agent definition has a name longer than 63 characters
- **THEN** its ACP slug is truncated to 63 characters with no trailing hyphen

### Requirement: Live agent status metrics
Agent manifests SHALL include a `status` object with live computed metrics: `avg_run_tokens` (average token usage), `avg_run_time_seconds` (average run duration), `success_rate` (ratio of successful runs to total runs). Metrics SHALL be computed from runs in the last 30 days.

#### Scenario: Status metrics computed from recent runs
- **WHEN** an agent has 10 runs in the last 30 days with 8 successes and average duration of 5.2 seconds
- **THEN** the manifest `status` object contains `success_rate: 0.8` and `avg_run_time_seconds: 5.2`

#### Scenario: Agent with no runs has null status
- **WHEN** an agent has no runs in the last 30 days
- **THEN** the manifest `status` field is `null` or omitted

### Requirement: Auth scopes for discovery endpoints
All ACP discovery endpoints (except `/acp/v1/ping`) SHALL require a valid `Bearer emt_*` API token with the `agents:read` scope. The project context SHALL be determined from the API token's associated project.

#### Scenario: Valid token with agents:read scope succeeds
- **WHEN** a client sends `GET /acp/v1/agents` with a valid token having `agents:read` scope
- **THEN** the server responds with HTTP 200 and agents from the token's project

#### Scenario: Token without agents:read scope is rejected
- **WHEN** a client sends `GET /acp/v1/agents` with a valid token that has `graph:read` but not `agents:read`
- **THEN** the server responds with HTTP 403
