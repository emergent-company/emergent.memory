# github-app-integration Specification

## Purpose
TBD - created by archiving change agent-workspace-infrastructure. Update Purpose after archive.
## Requirements
### Requirement: GitHub App manifest flow (one-click setup)

The system SHALL provide a one-click "Connect GitHub" flow using GitHub's App Manifest creation, eliminating manual credential configuration.

#### Scenario: Admin initiates GitHub connection

- **WHEN** an admin clicks "Connect GitHub" in the admin settings UI
- **THEN** the system generates a GitHub App manifest with required permissions (`contents:read`, `contents:write`), redirects the admin to `https://github.com/settings/apps/new?manifest=<encoded>`, and waits for the callback

#### Scenario: GitHub callback with credentials

- **WHEN** GitHub redirects back to Emergent with a temporary `code` after the admin completes App creation
- **THEN** the system exchanges the code via `POST https://api.github.com/app-manifests/{code}/conversions` to receive `app_id`, `pem` (private key), `webhook_secret`, and `app_slug`, stores all credentials encrypted in `core.github_app_config`, and returns success to the admin UI

#### Scenario: GitHub App installation

- **WHEN** the admin installs the newly created GitHub App on their organization or specific repositories
- **THEN** the system receives the `installation_id` via webhook (`installation.created` event), updates `core.github_app_config` with the `installation_id` and `installation_org`, and the connection status shows "Connected"

#### Scenario: Manifest flow failure

- **WHEN** the GitHub manifest flow fails (user cancels, network error, invalid manifest)
- **THEN** the system returns a clear error message to the admin UI, no partial credentials are stored, and the admin can retry

### Requirement: Encrypted credential storage

The system SHALL store GitHub App credentials encrypted at rest in the database with no plaintext exposure.

#### Scenario: Private key encryption

- **WHEN** the GitHub App private key (PEM) is received from the manifest flow
- **THEN** the system encrypts it using AES-256-GCM with a server-managed encryption key before storing in `core.github_app_config.private_key_encrypted`, and the plaintext PEM is never written to disk or logged

#### Scenario: Credential retrieval

- **WHEN** the system needs the private key to generate installation tokens
- **THEN** it decrypts the PEM from the database in-memory, uses it to sign a JWT, and the decrypted value is never cached beyond the token generation operation

#### Scenario: Credential deletion on disconnect

- **WHEN** an admin clicks "Disconnect" in the GitHub settings UI
- **THEN** the system deletes all rows from `core.github_app_config`, optionally revokes the GitHub App via API, and the connection status shows "Not Connected"

### Requirement: Installation access token generation

The system SHALL generate short-lived GitHub installation access tokens for repository operations, never using long-lived credentials directly.

#### Scenario: Token generation for git clone

- **WHEN** a workspace creation requires cloning a private repository
- **THEN** the system generates a JWT from the stored `app_id` and `private_key`, exchanges it for an installation access token (1-hour expiry) via `POST /app/installations/{installation_id}/access_tokens`, and uses the token for the git clone operation

#### Scenario: Token caching

- **WHEN** multiple workspace operations need GitHub access within the same hour
- **THEN** the system caches the installation token in memory for 55 minutes (5-minute safety margin before 1-hour expiry), reusing it for subsequent operations without re-generating

#### Scenario: Token refresh on expiry

- **WHEN** a cached installation token has expired or is within 5 minutes of expiry
- **THEN** the system generates a fresh installation token automatically without any admin intervention

#### Scenario: Token generation without installation

- **WHEN** a workspace requires GitHub access but no GitHub App is installed (no `installation_id` in config)
- **THEN** the system returns a clear error "GitHub App not installed — connect GitHub in Settings > Integrations" and the workspace is created without code

### Requirement: Admin settings UI

The system SHALL provide an admin settings page for GitHub integration with connection status, connect/disconnect actions, and diagnostic information.

#### Scenario: Display connected status

- **WHEN** a GitHub App is configured and installed
- **THEN** the admin settings page shows: connection status (green), app name and ID, organization name, number of accessible repositories, who connected it and when

#### Scenario: Display disconnected status

- **WHEN** no GitHub App is configured
- **THEN** the admin settings page shows: connection status (gray), a "Connect GitHub" button, explanation of what connecting enables, and a note about CLI fallback for self-hosted deployments

#### Scenario: Reconnect flow

- **WHEN** the admin clicks "Reconnect" on an existing connection
- **THEN** the system deletes the current credentials, initiates a new manifest flow, and completes the full connect cycle without requiring manual cleanup

### Requirement: CLI setup fallback

The system SHALL support CLI-based credential configuration for air-gapped or self-hosted environments where browser-based manifest flow is not available.

#### Scenario: CLI credential setup

- **WHEN** an admin runs `emergent github setup --app-id 12345 --private-key-file ./app.pem --installation-id 67890`
- **THEN** the CLI reads the PEM file, sends the credentials to `POST /api/v1/settings/github/cli`, the server encrypts and stores them in `core.github_app_config`, and confirms success

#### Scenario: CLI setup validation

- **WHEN** CLI credentials are submitted
- **THEN** the system validates the credentials by generating a test installation token and making a `GET /installation` API call, returning an error if validation fails

#### Scenario: CLI setup overwrites existing

- **WHEN** CLI setup is run while a GitHub App is already configured
- **THEN** the system replaces the existing credentials with the new ones after validation succeeds, and logs the change for audit

### Requirement: Bot commit identity

The system SHALL use the GitHub App's bot identity for all commits made through agent workspaces.

#### Scenario: Commit authorship

- **WHEN** an agent creates a git commit in a workspace that has a GitHub App configured
- **THEN** the commit is authored as `emergent-app[bot] <{app_id}+emergent-app[bot]@users.noreply.github.com>`, matching GitHub's standard App bot identity format

#### Scenario: Commit without GitHub App

- **WHEN** an agent creates a git commit in a workspace without a GitHub App configured
- **THEN** the commit uses a default identity `Emergent Agent <agent@emergent.local>` and the system logs a warning about missing GitHub App configuration

