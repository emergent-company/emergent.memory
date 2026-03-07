## ADDED Requirements

### Requirement: Create Account-Level API Token
The system SHALL allow authenticated users to create API tokens that are not bound to any specific project, granting access across all projects the user can access.

#### Scenario: Create account token without project
- **WHEN** an authenticated user sends `POST /api/tokens` with `{"name": "my-ci-token", "scopes": ["projects:read"]}`
- **THEN** the system SHALL return 201 with the generated token string, token ID, name, scopes, and `project_id: null`
- **AND** the token SHALL be stored with `project_id = NULL` in `core.api_tokens`
- **AND** the token string SHALL follow the `emt_<64-hex-chars>` format

#### Scenario: Account token name must be unique per user
- **WHEN** a user creates a second account token with the same name as an existing (non-revoked) token belonging to that user
- **THEN** the system SHALL return 409 Conflict
- **AND** the error message SHALL indicate the name is already in use

#### Scenario: Account token requires valid scopes
- **WHEN** a user sends `POST /api/tokens` with an unrecognized scope value
- **THEN** the system SHALL return 400 Bad Request
- **AND** the error message SHALL list the valid scope values

#### Scenario: Token secret returned only at creation
- **WHEN** an account token is successfully created
- **THEN** the full token string SHALL be returned exactly once in the creation response
- **AND** subsequent GET requests SHALL NOT return the token string, only the prefix and metadata

### Requirement: List Account-Level API Tokens
The system SHALL allow authenticated users to list all account-level tokens they own.

#### Scenario: List returns only account tokens for the authenticated user
- **WHEN** an authenticated user sends `GET /api/tokens`
- **THEN** the system SHALL return 200 with an array of token metadata objects
- **AND** each object SHALL include: `id`, `name`, `scopes`, `token_prefix`, `created_at`, `last_used_at`, `revoked_at`
- **AND** the response SHALL NOT include the full token string
- **AND** the response SHALL include only tokens where `project_id IS NULL` belonging to the calling user

#### Scenario: List returns empty array when no account tokens exist
- **WHEN** an authenticated user has no account-level tokens
- **AND** the user sends `GET /api/tokens`
- **THEN** the system SHALL return 200 with an empty array

### Requirement: Revoke Account-Level API Token
The system SHALL allow authenticated users to revoke their account-level tokens.

#### Scenario: Revoke own account token
- **WHEN** an authenticated user sends `DELETE /api/tokens/:tokenId`
- **AND** the token belongs to that user and has `project_id IS NULL`
- **THEN** the system SHALL set `revoked_at` to the current timestamp
- **AND** return 204 No Content
- **AND** the token SHALL no longer authenticate subsequent requests

#### Scenario: Cannot revoke another user's token
- **WHEN** a user sends `DELETE /api/tokens/:tokenId`
- **AND** the token belongs to a different user
- **THEN** the system SHALL return 404 Not Found

#### Scenario: Revoking already-revoked token is idempotent
- **WHEN** a user sends `DELETE /api/tokens/:tokenId` for an already-revoked token
- **THEN** the system SHALL return 204 No Content

### Requirement: Account-Level Token Scopes
The system SHALL support two new scope values for account-level tokens that govern project-level access.

#### Scenario: projects:read scope grants project listing
- **WHEN** a request is made with an account token bearing `projects:read` scope
- **AND** the request is `GET /api/projects`
- **THEN** the system SHALL return all projects the token owner is a member of

#### Scenario: projects:write scope grants project mutation
- **WHEN** a request is made with an account token bearing `projects:write` scope
- **AND** the request is a project-mutating operation (create, update, delete)
- **THEN** the system SHALL authorize the operation

#### Scenario: Account token without projects:read cannot list projects
- **WHEN** a request is made with an account token that does NOT bear `projects:read` or `projects:write`
- **AND** the request is `GET /api/projects`
- **THEN** the system SHALL return 403 Forbidden

### Requirement: Account Token Cross-Project Access
An account token with appropriate scopes SHALL authenticate requests to any project the owning user is a member of, without being restricted to a single project.

#### Scenario: Account token accesses multiple projects
- **WHEN** a user owns projects A and B
- **AND** the user creates an account token with `data:read` scope
- **THEN** the token SHALL successfully authenticate `GET /api/projects/A/objects`
- **AND** the token SHALL successfully authenticate `GET /api/projects/B/objects`

#### Scenario: Account token respects user membership
- **WHEN** an account token is used to access project C
- **AND** the token owner is NOT a member of project C
- **THEN** the system SHALL return 403 Forbidden

### Requirement: Database Schema — Nullable project_id
The `core.api_tokens` table SHALL support a nullable `project_id` to accommodate account-level tokens.

#### Scenario: Migration makes project_id nullable
- **WHEN** the migration is applied
- **THEN** `core.api_tokens.project_id` SHALL accept NULL values
- **AND** all existing rows SHALL retain their current non-null `project_id` values

#### Scenario: Unique constraint is per user not per project
- **WHEN** the migration is applied
- **THEN** the unique constraint on `core.api_tokens` SHALL be `(user_id, name)` where `revoked_at IS NULL`
- **AND** the old `(project_id, name)` constraint SHALL be removed
