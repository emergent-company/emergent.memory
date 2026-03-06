## MODIFIED Requirements

### Requirement: API Token Management

The system SHALL allow users to create, view, and revoke API tokens for programmatic MCP access at the project, organization, and account levels.

#### Scenario: User generates a new API token

- **WHEN** a user clicks "Generate Token" on the token management settings page
- **THEN** a modal appears prompting for a token name/description
- **AND** the user can select the token scope (Project, Organization, or Account) based on the context of the settings page they are viewing
- **AND** the user can select permissions (schema:read, data:read, data:write, agents:read, agents:write)
- **AND** upon confirmation, a new token is generated and displayed once
- **AND** the user is warned that the token will only be shown once

#### Scenario: User views existing API tokens

- **WHEN** a user views the API tokens section (within a project, organization, or account settings)
- **THEN** a table displays all tokens for the respective scope
- **AND** each row shows: token name, created date, last used date, scope, permissions, and a revoke button
- **AND** the actual token value is NOT displayed (only shown once at creation)

#### Scenario: User revokes an API token

- **WHEN** a user clicks "Revoke" on an existing token
- **THEN** a confirmation dialog appears
- **AND** upon confirmation, the token is immediately invalidated
- **AND** subsequent API calls with that token return 401 Unauthorized

### Requirement: API Token Backend Support

The backend SHALL provide endpoints for API token management across different scopes and validate API tokens alongside JWT tokens.

#### Scenario: Create API token endpoint

- **WHEN** a `POST /api/projects/:projectId/tokens` request is made with valid JWT authentication
- **THEN** a new API token is created in the database
- **AND** the response includes the token value (only returned once) and token metadata
- **AND** the token is scoped to the specified project

#### Scenario: List API tokens endpoint

- **WHEN** a `GET /api/projects/:projectId/tokens` request is made
- **THEN** the response includes all tokens for the project
- **AND** token values are NOT included in the response (security)

#### Scenario: Revoke API token endpoint

- **WHEN** a `DELETE /api/projects/:projectId/tokens/:tokenId` request is made
- **THEN** the token is marked as revoked in the database
- **AND** subsequent authentication attempts with that token fail

#### Scenario: API token validation in MCP requests

- **WHEN** an MCP request is made with `Authorization: Bearer <api_token>`
- **THEN** the AuthService validates the token against the `api_tokens` table
- **AND** if valid, the request is processed with the token's associated user, organization, or project context depending on the token's scope
- **AND** if the token is revoked or invalid, a 401 Unauthorized response is returned
