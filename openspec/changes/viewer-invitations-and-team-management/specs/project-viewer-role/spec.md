## ADDED Requirements

### Requirement: Viewer role definition
The system SHALL define a `project_viewer` role for project memberships. The viewer role SHALL be stored in `kb.project_memberships.role` alongside the existing `project_admin` and `project_user` roles. The viewer role SHALL be immutable — a viewer cannot promote themselves.

#### Scenario: Viewer role stored in membership
- **WHEN** a user accepts a viewer invitation
- **THEN** a `kb.project_memberships` record is created with `role = 'project_viewer'`

### Requirement: Read-only scope enforcement for viewers
When a viewer authenticates with a viewer-scoped API token, the system SHALL restrict them to read-only operations. The token's scopes SHALL be limited to `data:read`, `schema:read`, `agents:read`, and `projects:read`. Any request requiring a write scope SHALL be rejected with HTTP 403.

#### Scenario: Viewer token rejected for write operation
- **WHEN** a request arrives with a viewer-scoped token and targets a write endpoint (e.g., document ingest)
- **THEN** the server responds with HTTP 403 Forbidden

#### Scenario: Viewer token accepted for read operation
- **WHEN** a request arrives with a viewer-scoped token and targets a read endpoint (e.g., document list)
- **THEN** the server processes the request normally

### Requirement: Viewer-scoped token creation
When a project member with `project_viewer` role calls `memory projects create-token`, the system SHALL only allow scopes within the read-only set (`data:read`, `schema:read`, `agents:read`, `projects:read`). Requesting any write scope SHALL be rejected.

#### Scenario: Viewer cannot create write-scoped token
- **WHEN** a user with `project_viewer` role attempts to create a token with `data:write` scope
- **THEN** the API responds with HTTP 403 and an error explaining the scope is not permitted for viewers

#### Scenario: Viewer can create read-scoped token
- **WHEN** a user with `project_viewer` role creates a token with only read scopes
- **THEN** the token is created successfully
