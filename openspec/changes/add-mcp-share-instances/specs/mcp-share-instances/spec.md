## Purpose

Lets a project define named, independently scoped MCP access instances — each binding one API token to a chosen tool allowlist and optional agent allowlist — so outside agents receive exactly the access intended and nothing more.

## ADDED Requirements

### Requirement: Create an MCP share instance

A project admin SHALL be able to create a named MCP share instance for a project via `POST /api/projects/:projectId/mcp/shares`. The request MUST accept a required `name`, an optional `description`, an optional `tools` array of memory tool names, and an optional `agents` array of agent IDs. On success the system MUST create the instance, mint one project-scoped API token for it, and return the instance ID, the fully-qualified MCP endpoint URL, and the raw token value exactly once.

#### Scenario: Create with an explicit tool allowlist

- **WHEN** a project admin posts a share instance with a name and a non-empty `tools` list
- **THEN** the instance is persisted with that allowlist and a bound API token is minted
- **THEN** the response contains the instance ID, the MCP URL, and the raw token value exactly once

#### Scenario: Create with no `tools` field

- **WHEN** a project admin posts a share instance without a `tools` field
- **THEN** the instance is created with an unrestricted (`null`) allowlist meaning all scope-permitted tools

#### Scenario: Empty tool allowlist

- **WHEN** a project admin posts a share instance with `"tools": []`
- **THEN** the system rejects the request with an unprocessable-entity error and creates no instance

#### Scenario: Non-admin cannot create

- **WHEN** a user without project admin rights posts a share instance
- **THEN** the system returns HTTP 403 Forbidden and creates no instance

#### Scenario: Duplicate name rejected

- **WHEN** a project admin creates a second instance whose name matches an existing instance in the same project
- **THEN** the system rejects the request with a conflict error and creates no instance

#### Scenario: Unknown tool name rejected

- **WHEN** a project admin posts a `tools` list containing a name that is not an includable memory tool
- **THEN** the system rejects the request with an unprocessable-entity error naming the unknown tool

### Requirement: List and read MCP share instances

The system SHALL expose `GET /api/projects/:projectId/mcp/shares` returning every share instance for the project, and `GET /api/projects/:projectId/mcp/shares/:id` returning one instance. Each instance representation MUST include its name, description, tool allowlist, agent allowlist, status, creation time, and last-used time, and MUST NOT include the token secret.

#### Scenario: List returns all instances

- **WHEN** a project admin lists share instances for a project with several instances
- **THEN** every instance is returned with its tool and agent allowlists and status

#### Scenario: List omits token secrets

- **WHEN** any instance is listed or read
- **THEN** the response contains no raw token value

#### Scenario: Read an instance from another project

- **WHEN** a user requests an instance ID belonging to a different project
- **THEN** the system returns a not-found response and reveals nothing about the other project

### Requirement: Update an MCP share instance

A project admin SHALL be able to update an instance's name, description, tool allowlist, and agent allowlist via `PATCH /api/projects/:projectId/mcp/shares/:id`. Changing the tool allowlist MUST re-derive the bound token's scopes so the token's granted scopes match the selected tools. Updating an instance MUST NOT change its existing token secret.

#### Scenario: Update the tool allowlist

- **WHEN** an admin replaces an instance's `tools` with a different valid set
- **THEN** the instance stores the new allowlist and its bound token's scopes are updated to cover exactly the selected tools

#### Scenario: Rename an instance

- **WHEN** an admin changes an instance's name to a unique value
- **THEN** the new name is persisted and returned

#### Scenario: Rename to an existing name

- **WHEN** an admin renames an instance to a name already used by another instance in the project
- **THEN** the system rejects the request with a conflict error and keeps the previous name

#### Scenario: Update a revoked instance

- **WHEN** an admin attempts to update an instance that has been revoked
- **THEN** the system rejects the request and the instance remains revoked

### Requirement: Revoke an MCP share instance

A project admin SHALL be able to revoke an instance via `DELETE /api/projects/:projectId/mcp/shares/:id`. Revocation MUST immediately invalidate the bound API token so subsequent MCP requests authenticated with it are rejected. Revocation MUST be idempotent.

#### Scenario: Revoked token can no longer connect

- **WHEN** an admin revokes an instance and a client then calls the MCP endpoint with that instance's token
- **THEN** the MCP endpoint rejects the request as unauthenticated

#### Scenario: Repeated revoke is idempotent

- **WHEN** an admin revokes an instance that is already revoked
- **THEN** the system returns success and the instance remains revoked

### Requirement: Rotate an MCP share instance token

A project admin SHALL be able to rotate an instance's token via `POST /api/projects/:projectId/mcp/shares/:id/rotate`. Rotation MUST issue a new token with the instance's current scopes, invalidate the previous token, and return the new raw token value exactly once.

#### Scenario: Rotation invalidates the old token

- **WHEN** an admin rotates an instance's token and a client then connects with the previous token
- **THEN** the previous token is rejected

#### Scenario: Rotation preserves scoping

- **WHEN** an admin rotates the token of an instance with a tool allowlist
- **THEN** the new token carries the same scope set and the instance's allowlist is unchanged

### Requirement: Legacy read-only shares remain usable

Tokens created through the existing `POST /api/projects/:projectId/mcp/share` flow SHALL be represented as legacy instances with no explicit tool or agent allowlist, and SHALL continue to authenticate MCP requests with their existing read-only scopes. The system MUST NOT require legacy shares to be migrated before use.

#### Scenario: Legacy token still authenticates

- **WHEN** a client connects using a token minted by the legacy share endpoint
- **THEN** the MCP endpoint authenticates it and exposes all scope-permitted tools

#### Scenario: Legacy share appears in the instance list

- **WHEN** a project has a legacy share token
- **THEN** listing instances includes it and marks it as a legacy instance

### Requirement: Instance status reflects token lifecycle

An instance SHALL report a status derived from its bound token (active or revoked) and MUST NOT be usable once its token expires or is revoked.

#### Scenario: Expired token instance is not active

- **WHEN** an instance's bound token has passed its expiry time
- **THEN** the instance reports a non-active status and requests using that token are rejected
