## ADDED Requirements

### Requirement: Organization API Token Management
The system SHALL provide endpoints to create, list, and revoke API tokens scoped to an organization.

#### Scenario: Create Organization API token endpoint
- **WHEN** a `POST /api/orgs/:orgId/tokens` request is made with valid JWT authentication
- **THEN** a new API token is created in the database with `organization_id` set to the org ID and `project_id` set to null
- **AND** the response includes the token value (only returned once) and token metadata
- **AND** the token is scoped to the specified organization

#### Scenario: List Organization API tokens endpoint
- **WHEN** a `GET /api/orgs/:orgId/tokens` request is made
- **THEN** the response includes all tokens scoped to the organization
- **AND** token values are NOT included in the response (security)

#### Scenario: Revoke Organization API token endpoint
- **WHEN** a `DELETE /api/orgs/:orgId/tokens/:tokenId` request is made
- **THEN** the token is marked as revoked in the database
- **AND** subsequent authentication attempts with that token fail

### Requirement: Organization Token Authorization
The system SHALL validate organization tokens and grant access to resources within the organization.

#### Scenario: Validating an Organization Token
- **WHEN** an API or MCP request is made with an organization API token
- **THEN** the AuthService validates the token and sets the request context to the organization
- **AND** the token grants access to any project within that organization, according to the token's scopes
