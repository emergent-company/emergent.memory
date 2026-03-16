## Requirements

### Requirement: OIDC token revocation on logout
The system SHALL attempt to revoke the stored OAuth access token and refresh token via the OIDC revocation endpoint (RFC 7009) before deleting local credentials.

#### Scenario: Successful token revocation
- **WHEN** user runs `memory logout` and valid credentials with an issuer URL exist in `~/.memory/credentials.json`
- **THEN** the system SHALL discover the OIDC revocation endpoint from the stored issuer URL, POST revocation requests for the refresh token and access token, and then delete the local credentials file

#### Scenario: Revocation endpoint not available
- **WHEN** user runs `memory logout` and the OIDC discovery document does not include a `revocation_endpoint`
- **THEN** the system SHALL print a warning to stderr, skip server-side revocation, and proceed to delete the local credentials file

#### Scenario: Revocation request fails
- **WHEN** user runs `memory logout` and the revocation HTTP request fails (network error, non-2xx response, timeout)
- **THEN** the system SHALL print a warning to stderr and proceed to delete the local credentials file

#### Scenario: No issuer URL in credentials
- **WHEN** user runs `memory logout` and the stored credentials do not contain an issuer URL (e.g., set via `memory set-token`)
- **THEN** the system SHALL skip server-side revocation and proceed to delete the local credentials file

#### Scenario: Revocation timeout
- **WHEN** the revocation endpoint does not respond within 10 seconds
- **THEN** the system SHALL abort the revocation request, print a warning to stderr, and proceed to delete the local credentials file

### Requirement: Clear all auth state with --all flag
The system SHALL provide an `--all` flag on the `memory logout` command that clears all locally stored authentication state, including OAuth credentials, API key, and project token.

#### Scenario: Logout with --all flag
- **WHEN** user runs `memory logout --all`
- **THEN** the system SHALL delete `~/.memory/credentials.json` (with revocation attempt), clear the `api_key` and `project_token` fields in `~/.memory/config.yaml`, and report what was cleared

#### Scenario: Logout with --all when no config exists
- **WHEN** user runs `memory logout --all` and `~/.memory/config.yaml` does not exist
- **THEN** the system SHALL delete OAuth credentials (if present) and report that no config file was found to clear

#### Scenario: Logout with --all preserves non-auth config
- **WHEN** user runs `memory logout --all` and `~/.memory/config.yaml` contains `server_url`, `project_id`, UI preferences, and other non-auth fields
- **THEN** the system SHALL only clear `api_key` and `project_token` fields, preserving all other configuration values

### Requirement: Detailed logout output
The system SHALL print a summary of actions taken during logout, indicating which credentials were cleared.

#### Scenario: Default logout output
- **WHEN** user runs `memory logout` and OAuth credentials are deleted
- **THEN** the system SHALL print confirmation that OAuth credentials were removed, including whether token revocation succeeded or was skipped

#### Scenario: --all logout output
- **WHEN** user runs `memory logout --all`
- **THEN** the system SHALL print confirmation for each type of credential cleared: OAuth credentials, API key (if was set), project token (if was set)
