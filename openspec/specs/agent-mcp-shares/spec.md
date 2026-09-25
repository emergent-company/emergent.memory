# agent-mcp-shares Specification

## Purpose
Defines how a project admin manages the credentials of an agent's own MCP endpoint: many labeled keys on one endpoint, each individually created, listed, revoked, and rotated, superseding the former single-credential `core.agent_mcp_shares` share.

## Requirements

### Requirement: Create a per-agent MCP share credential

A project admin SHALL be able to create a key on an agent's MCP endpoint. The request MUST accept a required `label`. On success the system MUST create a key bound to the endpoint, mint a project API token for it, and return the key details, the endpoint URL for that agent, and the raw token value exactly once. Because an agent has exactly one active endpoint, creating the endpoint itself is idempotent: creating a key for an agent with no active endpoint MUST first establish that endpoint.

#### Scenario: Create returns the token once

- **WHEN** an admin creates a key for an agent endpoint
- **THEN** the response contains the key id, the endpoint URL for that agent, and the raw token value exactly once

#### Scenario: Endpoint is established on first key

- **WHEN** an admin creates a key for an agent that has no active endpoint
- **THEN** the endpoint is created and the key is bound to it

#### Scenario: Non-admin denied

- **WHEN** a user without project admin rights attempts to create a key
- **THEN** the system returns HTTP 403 and creates nothing

#### Scenario: Unknown or foreign agent rejected

- **WHEN** the named agent does not exist in the project
- **THEN** the system returns a not-found/unprocessable error and creates nothing

#### Scenario: Duplicate label rejected

- **WHEN** an admin creates a key whose label matches an active key's label on the same endpoint, ignoring case
- **THEN** the system rejects the request with a conflict error

#### Scenario: Default name when omitted

- **WHEN** an admin creates a key without a label
- **THEN** the system rejects the request with a validation error because a label is required

#### Scenario: Duplicate name rejected

- **WHEN** an admin creates a key whose label matches an active key's label on the same endpoint, ignoring case
- **THEN** the system rejects the request with a conflict error

### Requirement: List an agent's share credentials

The system SHALL expose a list of an endpoint's keys returning each key's label, status, creation time, and last-used time. List representations MUST NOT include the token secret. A project-wide view MAY list endpoints with their keys.

#### Scenario: List returns keys without secrets

- **WHEN** an admin lists keys for an endpoint
- **THEN** each active key is returned with its label, status, and timestamps and no raw token

#### Scenario: Revoked keys are marked

- **WHEN** a key has been revoked
- **THEN** it is listed with a revoked status

#### Scenario: List returns shares without secrets

- **WHEN** an admin lists keys for an endpoint
- **THEN** each active key is returned with its label, status, and timestamps and no raw token

#### Scenario: Revoked shares are marked

- **WHEN** a key has been revoked
- **THEN** it is listed with a revoked status

### Requirement: Revoke a share credential

A project admin SHALL be able to revoke one key of an endpoint. Revocation MUST immediately invalidate the bound token so it can no longer authenticate the per-agent endpoint, MUST leave the endpoint's other keys unaffected, and MUST be idempotent.

#### Scenario: Revoked token cannot connect

- **WHEN** a key is revoked and a client then connects with its token
- **THEN** the endpoint rejects the request

#### Scenario: Other keys remain usable

- **WHEN** one key of an endpoint is revoked
- **THEN** the endpoint's other keys continue to authenticate

#### Scenario: Repeated revoke is idempotent

- **WHEN** an already-revoked key is revoked again
- **THEN** the system returns success and the key remains revoked

### Requirement: Rotate a share credential

A project admin SHALL be able to rotate one key's token. Rotation MUST invalidate the previous token, issue a new token for the same key and endpoint, return the new raw token value exactly once, and MUST preserve the key identity so its existing sessions remain continuable.

#### Scenario: Rotation invalidates the previous token

- **WHEN** a key's token is rotated and a client connects with the previous token
- **THEN** the previous token is rejected

#### Scenario: New token authorizes the endpoint

- **WHEN** a key's token is rotated and a client connects with the new token
- **THEN** the endpoint authorizes the client and exposes the session catalog

#### Scenario: Rotation preserves sessions

- **WHEN** a key with existing sessions is rotated
- **THEN** those sessions remain continuable with the new token

### Requirement: Share status reflects token lifecycle

A share SHALL report an active or revoked status derived from its bound token, and MUST NOT authorize requests once its token is revoked or expired.

#### Scenario: Expired token is not active

- **WHEN** a share's bound token has expired
- **THEN** the share reports a non-active status and requests using it are rejected

### Requirement: Shares are project-scoped

A share SHALL belong to exactly one project and one agent. A user MUST NOT read, revoke, or rotate a share belonging to another project.

#### Scenario: Cross-project access denied

- **WHEN** a user requests or mutates a share id from a different project
- **THEN** the system returns not-found/forbidden and reveals nothing about the other project

### Requirement: core.agent_mcp_shares is superseded and backfilled

The single-credential `core.agent_mcp_shares` binding SHALL be superseded by the agent-owned endpoint and its many labeled keys. Existing active share rows MUST have been backfilled into one endpoint per distinct `(project_id, agent_id)` and one key per share row, copying the bound `token_id`, `created_by`, and `revoked_at`, with the key `label` taken from the share `name`, without modifying the bound tokens or their scopes so live keys keep working through the cutover. The legacy `core.agent_mcp_shares` table SHALL have been dropped once no reader remains, together with the unused `core.mcp_share_instances.allowed_agents` column. The tables backing the agent-owned endpoint (`core.agent_mcp_endpoints`, `core.agent_mcp_keys`, `core.agent_mcp_sessions`) and the project share instances (`core.mcp_share_instances`), including legacy rows, MUST remain.

#### Scenario: Active shares are backfilled

- **WHEN** the backfill migration runs over existing active shares
- **THEN** each distinct agent has one endpoint and each share becomes one labeled key bound to the same token

#### Scenario: Live keys keep working through cutover

- **WHEN** a client connects with a token that was minted for a backfilled share
- **THEN** the token authenticates the agent endpoint with no re-issuance

#### Scenario: The old table is dropped only after readers are gone

- **WHEN** the drop migration runs after the backfill has shipped and no reader remains
- **THEN** `core.agent_mcp_shares` and `core.mcp_share_instances.allowed_agents` no longer exist, and no code path reads or writes either
