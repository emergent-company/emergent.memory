## Purpose

Keeps project-scoped MCP share instances as a tools-only feature — a named API token bound to an explicit tool allowlist — and removes the agent allowlist and agent picker so that agent sharing is handled solely by the agent-scoped endpoint.

## MODIFIED Requirements

### Requirement: Create an MCP share instance

A project admin SHALL be able to create a named MCP share instance for a project via `POST /api/projects/:projectId/mcp/shares`. The request MUST accept a required `name`, an optional `description`, and an optional `tools` array of memory tool names. An instance scopes TOOLS ONLY: the request MUST NOT accept an `agents` array, and a request supplying one MUST be rejected. On success the system MUST create the instance, mint one project-scoped API token for it, and return the instance ID, the fully-qualified MCP endpoint URL, and the raw token value exactly once. Users who want to share an agent MUST be directed to the agent-scoped endpoint instead.

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

#### Scenario: Agent allowlist is rejected

- **WHEN** a project admin posts a share instance with an `agents` array
- **THEN** the system rejects the request with an unprocessable-entity error and creates no instance

#### Scenario: Agent sharing is redirected

- **WHEN** a user wants to expose one agent to an outside client
- **THEN** the system directs them to the agent-scoped endpoint and its keys rather than to a project share instance

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

The system SHALL expose `GET /api/projects/:projectId/mcp/shares` returning every share instance for the project, and `GET /api/projects/:projectId/mcp/shares/:id` returning one instance. Each instance representation MUST include its name, description, tool allowlist, status, creation time, and last-used time, and MUST NOT include the token secret. The representation MUST NOT include an agent allowlist, because instances no longer carry one.

#### Scenario: List returns all instances

- **WHEN** a project admin lists share instances for a project with several instances
- **THEN** every instance is returned with its tool allowlist and status

#### Scenario: List omits token secrets

- **WHEN** any instance is listed or read
- **THEN** the response contains no raw token value

#### Scenario: List omits agent allowlist

- **WHEN** any instance is listed or read
- **THEN** the response contains no agent allowlist or agent picker data

#### Scenario: Read an instance from another project

- **WHEN** a user requests an instance ID belonging to a different project
- **THEN** the system returns a not-found response and reveals nothing about the other project

### Requirement: Update an MCP share instance

A project admin SHALL be able to update an instance's name, description, and tool allowlist via `PATCH /api/projects/:projectId/mcp/shares/:id`. Changing the tool allowlist MUST re-derive the bound token's scopes so the token's granted scopes match the selected tools. The update MUST NOT accept or change an agent allowlist, and MUST NOT change the instance's existing token secret.

#### Scenario: Update the tool allowlist

- **WHEN** an admin replaces an instance's `tools` with a different valid set
- **THEN** the instance stores the new allowlist and its bound token's scopes are updated to cover exactly the selected tools

#### Scenario: Rename an instance

- **WHEN** an admin changes an instance's name to a unique value
- **THEN** the new name is persisted and returned

#### Scenario: Rename to an existing name

- **WHEN** an admin renames an instance to a name already used by another instance in the project
- **THEN** the system rejects the request with a conflict error and keeps the previous name

#### Scenario: Agent allowlist update is rejected

- **WHEN** an admin attempts to set or change an `agents` field on an instance
- **THEN** the system rejects the request with an unprocessable-entity error and the instance is unchanged

#### Scenario: Update a revoked instance

- **WHEN** an admin attempts to update an instance that has been revoked
- **THEN** the system rejects the request and the instance remains revoked
