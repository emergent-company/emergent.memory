# agent-mcp-keys Specification

## Purpose
Makes the per-agent MCP endpoint an object the agent owns, with many labeled credentials on one endpoint — each individually labeled, revocable, and rotatable, and each tracking when it was last used.

## Requirements

### Requirement: An agent owns exactly one endpoint

The system SHALL persist a per-agent MCP endpoint that belongs to exactly one project and one agent. At most one active endpoint SHALL exist per agent at a time, and a revoked endpoint SHALL not count against that limit. The endpoint SHALL be created and revoked from the agent's own configuration surface — the "MCP sharing" subpage of the agent Settings group at `/agents/:id/settings/mcp` — not only from a separate MCP-sharing page. Deleting the agent MUST delete its endpoint and everything the endpoint owns.

#### Scenario: One active endpoint per agent

- **WHEN** an endpoint already exists for an agent and a second active endpoint is created for the same agent
- **THEN** the system rejects the second create and the first endpoint remains active

#### Scenario: A revoked endpoint is replaced

- **WHEN** an agent's endpoint has been revoked and a new endpoint is created for that agent
- **THEN** the new endpoint is created and becomes the agent's active endpoint

#### Scenario: Deleting the agent removes the endpoint

- **WHEN** the agent row is deleted (agents are hard-deleted)
- **THEN** its endpoint and its keys and sessions are deleted as well, and no orphaned binding remains

#### Scenario: Endpoint is managed from the agent

- **WHEN** a project admin views an agent's MCP sharing settings subpage
- **THEN** the agent's MCP endpoint and its keys are created, listed, revoked, and rotated from that subpage

### Requirement: A key is a labeled credential on one endpoint

The system SHALL allow many keys on one endpoint. Each key MUST bind exactly one `core.api_tokens` row to the endpoint and MUST carry a human-readable label. A token MUST NOT be bound to more than one key, so one credential can never authorize two agents. The label MUST be unique among an endpoint's active keys, case-insensitively.

#### Scenario: Many keys on one endpoint

- **WHEN** a project admin creates several labeled keys for one agent endpoint
- **THEN** every key is listed for that endpoint and each independently authorizes the endpoint

#### Scenario: Duplicate label rejected

- **WHEN** a project admin creates a key whose label matches an active key's label on the same endpoint, ignoring case
- **THEN** the system rejects the request with a conflict error and creates no key

#### Scenario: A token cannot authorize two agents

- **WHEN** an attempt is made to bind an API token that is already bound to another key
- **THEN** the system rejects the request and no key authorizes the endpoint

### Requirement: The key secret is returned exactly once

Creating a key SHALL return the raw token value exactly once, in the create response only. Any list or read representation SHALL include the key label, status, creation time, and last-used time and MUST NOT include the token secret.

#### Scenario: Create returns the secret once

- **WHEN** a project admin creates a key
- **THEN** the response contains the key id, the endpoint URL, and the raw token value exactly once

#### Scenario: List never returns secrets

- **WHEN** any key is listed or read after creation
- **THEN** the response contains no raw token value

### Requirement: Keys are individually revocable

A project admin SHALL be able to revoke one key without affecting the endpoint or its other keys. Revocation MUST immediately invalidate the bound token so it can no longer authenticate the endpoint, and MUST be idempotent.

#### Scenario: Revoked key cannot connect

- **WHEN** a key is revoked and a client then connects with its token
- **THEN** the endpoint rejects the request

#### Scenario: Other keys are unaffected

- **WHEN** one of an endpoint's keys is revoked
- **THEN** the endpoint's other keys continue to authorize the endpoint

#### Scenario: Repeated revoke is idempotent

- **WHEN** an already-revoked key is revoked again
- **THEN** the system returns success and the key remains revoked

### Requirement: Keys are individually rotatable and rotation preserves sessions

A project admin SHALL be able to rotate one key's token. Rotation MUST invalidate the previous token, issue a new token for the same key and endpoint, and return the new raw token value exactly once. Rotation MUST preserve the key identity so the key's existing sessions remain continuable.

#### Scenario: Rotation invalidates the previous token

- **WHEN** a key is rotated and a client connects with the previous token
- **THEN** the previous token is rejected

#### Scenario: New token authorizes the endpoint

- **WHEN** a key is rotated and a client connects with the new token
- **THEN** the endpoint authorizes the client

#### Scenario: Sessions survive rotation

- **WHEN** a key with existing sessions is rotated
- **THEN** those sessions remain continuable with the rotated key

### Requirement: Key status and last-used time reflect the token lifecycle

A key SHALL report a status derived from its bound token (active or revoked) and MUST NOT authorize requests once its token is revoked or expired. The system SHALL surface the token's last-used time for a key without duplicating its own expiry or last-used columns.

#### Scenario: Expired token is not active

- **WHEN** a key's bound token has passed its expiry time
- **THEN** the key reports a non-active status and requests using it are rejected

#### Scenario: Last-used time is surfaced

- **WHEN** a client authenticates with a key
- **THEN** the key's last-used time reflects that use

### Requirement: Keys are project-scoped

A key SHALL belong to exactly one project through its endpoint. A user MUST NOT read, revoke, or rotate a key belonging to another project, and a key MUST be rejected when presented to an endpoint that binds a different agent.

#### Scenario: Cross-project access denied

- **WHEN** a user requests or mutates a key from a different project
- **THEN** the system returns not-found/forbidden and reveals nothing about the other project

#### Scenario: Key for another agent is rejected

- **WHEN** a client uses a key bound to agent A against the endpoint for agent B
- **THEN** the system rejects the request
