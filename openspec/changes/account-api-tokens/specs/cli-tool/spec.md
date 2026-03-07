## ADDED Requirements

### Requirement: Create Account Token via CLI
The CLI `memory tokens create` command SHALL support creating account-level tokens by making `--project` optional. When `--project` is omitted, an account token is created via `POST /api/tokens`.

#### Scenario: Create account token without --project flag
- **WHEN** a user runs `memory tokens create --name ci-token --scopes projects:read`
- **AND** the `--project` flag is not provided
- **THEN** the CLI SHALL call `POST /api/tokens` (not the project-scoped endpoint)
- **AND** display the generated token string with a warning to store it safely
- **AND** display the token name, scopes, and type as "account"

#### Scenario: Create project token with --project flag (unchanged behavior)
- **WHEN** a user runs `memory tokens create --project proj-123 --name my-token --scopes data:read`
- **THEN** the CLI SHALL call `POST /api/projects/proj-123/tokens` as before
- **AND** display the token string and metadata

#### Scenario: Invalid scope rejected at CLI level
- **WHEN** a user runs `memory tokens create --name t --scopes invalid:scope`
- **THEN** the CLI SHALL return an error listing valid scope values before making any API call

### Requirement: List Tokens Shows Account Tokens
The CLI `memory tokens list` command SHALL display both account-level and project-level tokens, with a column indicating type.

#### Scenario: List includes token type column
- **WHEN** a user runs `memory tokens list`
- **THEN** the output table SHALL include a "Type" column
- **AND** account tokens SHALL show "account" in the Type column
- **AND** project tokens SHALL show "project" in the Type column

#### Scenario: List account tokens without --project flag
- **WHEN** a user runs `memory tokens list` without `--project`
- **THEN** the CLI SHALL call `GET /api/tokens` and display account tokens

#### Scenario: List project tokens with --project flag
- **WHEN** a user runs `memory tokens list --project proj-123`
- **THEN** the CLI SHALL call `GET /api/projects/proj-123/tokens` as before

### Requirement: Revoke Account Token via CLI
The CLI `memory tokens revoke` command SHALL support revoking account-level tokens via `DELETE /api/tokens/:tokenId`.

#### Scenario: Revoke account token by ID
- **WHEN** a user runs `memory tokens revoke <token-id>` without `--project`
- **THEN** the CLI SHALL call `DELETE /api/tokens/<token-id>`
- **AND** display "Token revoked."

#### Scenario: Revoke project token with --project flag (unchanged)
- **WHEN** a user runs `memory tokens revoke <token-id> --project proj-123`
- **THEN** the CLI SHALL call the existing project-scoped revoke endpoint
