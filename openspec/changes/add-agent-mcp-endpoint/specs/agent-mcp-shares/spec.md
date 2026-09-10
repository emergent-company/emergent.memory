## Purpose

Lets a project admin generate, list, revoke, and rotate an API credential that exposes one chosen agent as a single-tool MCP server.

## ADDED Requirements

### Requirement: Create a per-agent MCP share credential

A project admin SHALL be able to create a share credential for an agent via `POST /api/projects/:projectId/agents/:agentId/mcp-share`. The request MAY carry a `name` and `description`. On success the system MUST create a record binding a newly minted project API token to that agent and return the share details, the per-agent MCP endpoint URL, and the raw token value exactly once.

#### Scenario: Create returns the token once

- **WHEN** an admin creates a share for an agent
- **THEN** the response contains the share id, the endpoint URL for that agent, and the raw token value exactly once

#### Scenario: Default name when omitted

- **WHEN** an admin creates a share without a name
- **THEN** the system assigns a default name that identifies the agent and date

#### Scenario: Non-admin denied

- **WHEN** a user without project admin rights attempts to create a share
- **THEN** the system returns HTTP 403 and creates nothing

#### Scenario: Unknown or foreign agent rejected

- **WHEN** the named agent does not exist in the project
- **THEN** the system returns a not-found/unprocessable error and creates nothing

#### Scenario: Duplicate name rejected

- **WHEN** an admin creates a share whose name matches an existing active share in the project
- **THEN** the system rejects the request with a conflict error

### Requirement: List an agent's share credentials

The system SHALL expose `GET /api/projects/:projectId/agents/:agentId/mcp-shares` (and a project-wide list) returning each share's name, description, status, creation time, and last-used time. List representations MUST NOT include the token secret.

#### Scenario: List returns shares without secrets

- **WHEN** an admin lists shares for an agent
- **THEN** each active share is returned with its status and timestamps and no raw token

#### Scenario: Revoked shares are marked

- **WHEN** a share has been revoked
- **THEN** it is listed with a revoked status

### Requirement: Revoke a share credential

A project admin SHALL be able to revoke a share via `DELETE /api/projects/:projectId/agent-mcp-shares/:id`. Revocation MUST immediately invalidate the bound token so it can no longer authenticate the per-agent endpoint, and MUST be idempotent.

#### Scenario: Revoked token cannot connect

- **WHEN** a share is revoked and a client then connects with its token
- **THEN** the endpoint rejects the request

#### Scenario: Repeated revoke is idempotent

- **WHEN** an already-revoked share is revoked again
- **THEN** the system returns success and the share remains revoked

### Requirement: Rotate a share credential

A project admin SHALL be able to rotate a share's token via `POST /api/projects/:projectId/agent-mcp-shares/:id/rotate`. Rotation MUST invalidate the previous token, issue a new one with the same binding, and return the new raw token value exactly once.

#### Scenario: Rotation invalidates the previous token

- **WHEN** a share's token is rotated and a client connects with the previous token
- **THEN** the previous token is rejected

#### Scenario: New token authorizes the endpoint

- **WHEN** a share's token is rotated and a client connects with the new token
- **THEN** the endpoint authorizes the client and exposes `call_agent`

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
