## ADDED Requirements

### Requirement: Agent MCP endpoint bundle download

The system SHALL expose a public `GET /api/mcp/agents/:agentId/bundle` route that returns a `.mcpb` MCP Bundle for the agent's MCP endpoint, parameterised by the agent key as a `token` query parameter. The route SHALL require no session: the key is the credential. It MUST resolve the token (SHA-256 hash lookup against `core.api_tokens`, active and non-expired) to an API token id, then require an ACTIVE agent key bound to that token and to the named agent, with the endpoint not revoked. It MUST reject with 400 when the token parameter is missing, 401 when the token is unknown/expired/revoked/unbound, and 403 when the key is bound to a different agent or the agent is unavailable/disabled. The bundle's MCP server URL MUST be the per-agent endpoint (`.../api/mcp/agents/:agentId`), not the project endpoint.

#### Scenario: Valid agent key downloads a bundle

- **WHEN** a client requests the bundle with the token of an active key bound to the named agent
- **THEN** the response is a `.mcpb` attachment whose manifest points at that agent's `/api/mcp/agents/:agentId` endpoint

#### Scenario: Revoked or expired token is rejected

- **WHEN** the token is unknown, revoked, or expired
- **THEN** the system returns 401 and no bundle

#### Scenario: Key bound to a different agent is rejected

- **WHEN** the token is bound to a different agent than the URL names
- **THEN** the system returns 403 and no bundle

#### Scenario: Missing token is rejected

- **WHEN** the request omits the token parameter
- **THEN** the system returns 400 and no bundle

### Requirement: Agent endpoint docs advertise the external origin

Every URL the web UI shows for an agent's MCP endpoint — the endpoint URL box, the generated client snippets, and the bundle download link — MUST use the memory server's externally-reachable origin (the configured `MEMORY_PUBLIC_URL`, defaulting to `MEMORY_URL`), regardless of the host the backend reports. A backend URL carrying an internal host MUST NOT be rendered to the user.

#### Scenario: Internal host is not shown

- **WHEN** the backend reports the agent endpoint URL on an internal host
- **THEN** the endpoint URL box, snippets, and bundle link use the memory server's external origin instead
