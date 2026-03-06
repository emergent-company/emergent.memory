## ADDED Requirements

### Requirement: Account API Token Management
The system SHALL provide endpoints to create, list, and revoke API tokens scoped to a user's account.

#### Scenario: Create Account API token endpoint
- **WHEN** a `POST /api/user/tokens` request is made with valid JWT authentication
- **THEN** a new API token is created in the database with both `project_id` and `organization_id` set to null
- **AND** the response includes the token value (only returned once) and token metadata
- **AND** the token is scoped to the authenticated user's account

#### Scenario: List Account API tokens endpoint
- **WHEN** a `GET /api/user/tokens` request is made
- **THEN** the response includes all account-scoped tokens for the user
- **AND** token values are NOT included in the response (security)

#### Scenario: Revoke Account API token endpoint
- **WHEN** a `DELETE /api/user/tokens/:tokenId` request is made
- **THEN** the token is marked as revoked in the database
- **AND** subsequent authentication attempts with that token fail

### Requirement: Account Token Authorization
The system SHALL validate account tokens and grant access to any resource the user has access to.

#### Scenario: Validating an Account Token
- **WHEN** an API or MCP request is made with an account API token
- **THEN** the AuthService validates the token and sets the request context to the user
- **AND** the token grants access to any organization or project the user belongs to, according to the token's scopes
