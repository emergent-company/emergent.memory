## ADDED Requirements

### Requirement: Global AgentCard discovery endpoint
The system SHALL expose an unauthenticated `GET /.well-known/agent-card.json` returning a valid A2A v1.0 `AgentCard` JSON document. The response SHALL be tenant-blind and SHALL NOT be derived from any project, agent-definition, or organization record.

#### Scenario: Global card is served without authentication
- **WHEN** a client sends `GET /.well-known/agent-card.json` with no `Authorization` header
- **THEN** the server responds with HTTP 200 and a JSON body conforming to the A2A v1.0 `AgentCard` schema

#### Scenario: Global card leaks no tenant data
- **WHEN** any project contains agent definitions with recognizable names or IDs
- **THEN** the global AgentCard body contains none of those names, project IDs, or organization IDs

#### Scenario: Global card advertises the HTTP+JSON interface
- **WHEN** a client fetches the global AgentCard
- **THEN** `supportedInterfaces` contains exactly one entry with `protocolBinding: "HTTP+JSON"` and `protocolVersion: "1.0"`

#### Scenario: Global card declares extended-card support
- **WHEN** a client fetches the global AgentCard
- **THEN** `capabilities.extendedAgentCard` is `true` and `capabilities.streaming` is `true`

### Requirement: Authenticated extended AgentCard
The system SHALL expose `GET /extendedAgentCard` returning the A2A `AgentCard` for the authenticated project, requiring a valid `Bearer emt_*` token with the `agents:read` scope. The extended card SHALL populate `skills[]` with one `AgentSkill` per agent definition whose `visibility` is `external` in that project.

#### Scenario: Extended card lists external agents as skills
- **WHEN** an authenticated client sends `GET /extendedAgentCard` and the project has three agent definitions — one `external`, one `project`, one `internal`
- **THEN** the response contains exactly one entry in `skills[]`, corresponding to the `external` definition

#### Scenario: Extended card requires authentication
- **WHEN** a client sends `GET /extendedAgentCard` without an `Authorization` header
- **THEN** the server responds with HTTP 401

#### Scenario: Extended card requires agents:read scope
- **WHEN** a client sends `GET /extendedAgentCard` with a valid token lacking `agents:read`
- **THEN** the server responds with HTTP 403

### Requirement: A2A AgentSkill derivation
Each `AgentSkill` in the extended card SHALL include the required A2A fields `id`, `name`, `description`, and `tags`. The `id` SHALL be the RFC 1123 DNS-label slug derived from the agent definition name (`pkg/acpslug`), `name` and `description` SHALL prefer `ACPConfig` values and fall back to the definition fields, and `inputModes`/`outputModes` SHALL be populated from `ACPConfig` when present.

#### Scenario: Skill uses slug as id
- **WHEN** an external definition is named `My Cool Agent`
- **THEN** its `AgentSkill.id` is `my-cool-agent`

#### Scenario: Skill description prefers ACPConfig
- **WHEN** an external definition has both a definition description and an `ACPConfig` description
- **THEN** the `AgentSkill.description` uses the `ACPConfig` value

#### Scenario: Skill tags are always present
- **WHEN** an external definition has no configured tags
- **THEN** the `AgentSkill.tags` field is present (an empty array is acceptable) and the skill still validates against the A2A schema

### Requirement: Discovery security declaration
The extended AgentCard SHALL declare a bearer `httpAuthSecurityScheme` and a corresponding `securityRequirements` entry describing how clients authenticate. It SHALL NOT declare an OAuth2 or OpenID Connect scheme that the server does not implement.

#### Scenario: Extended card declares bearer auth
- **WHEN** a client fetches `GET /extendedAgentCard`
- **THEN** `securitySchemes` contains an entry whose `httpAuthSecurityScheme.scheme` is `Bearer`

#### Scenario: No unbacked OAuth scheme is declared
- **WHEN** the A2A integration is configured without an OAuth2/OIDC provider
- **THEN** neither `securitySchemes` nor `securityRequirements` contains an `oauth2SecurityScheme` or `openIdConnectSecurityScheme`

### Requirement: No tenant field in advertised interfaces
The AgentCard SHALL NOT advertise a `tenant` value in `supportedInterfaces[]`, because project addressing is derived from the bearer credential rather than from a path or interface parameter.

#### Scenario: Interfaces omit tenant
- **WHEN** a client fetches either the global or extended AgentCard
- **THEN** every entry in `supportedInterfaces` omits `tenant`

### Requirement: Discovery content type
The discovery endpoints SHALL respond with media type `application/a2a+json`, and SHALL accept clients that only send or expect `application/json`.

#### Scenario: A2A content type is returned
- **WHEN** a client requests the AgentCard
- **THEN** the response `Content-Type` is `application/a2a+json`

#### Scenario: Plain JSON is tolerated
- **WHEN** a client requests the AgentCard with `Accept: application/json`
- **THEN** the server still responds with HTTP 200 and a valid AgentCard body
