## MODIFIED Requirements

### Requirement: API Token Validation Middleware
The system SHALL validate `emt_` tokens and establish the request context, supporting both project-bound tokens (existing) and account-level tokens (new, `project_id IS NULL`).

#### Scenario: Project-bound token sets project context (unchanged)
- **WHEN** a request arrives with a valid `emt_` token that has a non-null `project_id`
- **THEN** the middleware SHALL set `APITokenProjectID` in the request context to that project's ID
- **AND** `RequireProjectScope` SHALL restrict the token to operations on that project only

#### Scenario: Account token does not restrict to a single project
- **WHEN** a request arrives with a valid `emt_` token that has `project_id IS NULL`
- **THEN** the middleware SHALL NOT set `APITokenProjectID` in the request context (it SHALL remain empty)
- **AND** subsequent project-scope checks SHALL not reject the request based on project mismatch

#### Scenario: Account token with insufficient scope is rejected
- **WHEN** a request arrives with an account token
- **AND** the requested route requires a scope the token does not have
- **THEN** the middleware SHALL return 403 Forbidden

#### Scenario: Revoked account token is rejected
- **WHEN** a request arrives with a revoked account token (non-null `revoked_at`)
- **THEN** the middleware SHALL return 401 Unauthorized
