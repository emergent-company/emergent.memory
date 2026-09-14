# integrations Specification

## Purpose

Lets a signed-in user connect and manage third-party integrations for the active project: the GitHub App OAuth connect/status/disconnect flow plus general integration list, configure, test, sync, and delete.

## ADDED Requirements

### Requirement: GitHub App connection status

The gateway SHALL show whether the GitHub App is connected for the active project, and SHALL NOT expose any GitHub credential or token in that status.

#### Scenario: GitHub connected

- **WHEN** a signed-in user opens the integrations screen and the active project has a connected GitHub App
- **THEN** the gateway shows the GitHub App as connected without revealing the underlying token

#### Scenario: GitHub not connected

- **WHEN** a signed-in user opens the integrations screen and the active project has no connected GitHub App
- **THEN** the gateway shows the GitHub App as disconnected and offers a connect action

### Requirement: Connect GitHub App via OAuth

The gateway SHALL start the GitHub OAuth flow by requesting an authorization URL from Memory and redirecting the user's browser to it. The OAuth code exchange and token storage MUST occur on Memory, not in the gateway.

#### Scenario: Connect flow started

- **WHEN** a signed-in user initiates the GitHub connect action
- **THEN** the gateway obtains an authorization URL from Memory and redirects the browser to GitHub

#### Scenario: Callback handled by Memory

- **WHEN** GitHub redirects back with an authorization code after the user grants access
- **THEN** Memory receives the callback, exchanges the code, stores the credential, and the gateway reflects the resulting connection status

#### Scenario: Connect failure surfaced

- **WHEN** the connect request fails (e.g. Memory rejects it or is unreachable)
- **THEN** the gateway surfaces the failure to the user and keeps the GitHub App in the disconnected state

### Requirement: Disconnect GitHub App

The gateway SHALL let the user disconnect the GitHub App for the active project.

#### Scenario: Disconnect

- **WHEN** a signed-in user disconnects a connected GitHub App
- **THEN** Memory drops the stored GitHub credential and the gateway shows the GitHub App as disconnected

### Requirement: Distinguish available from configured integrations

The gateway SHALL list both the catalog of integration types that can be configured for the project and the project's configured integration instances, so the user can tell what is installed from what is installable.

#### Scenario: Catalog and configured lists

- **WHEN** a signed-in user opens the integrations screen
- **THEN** the gateway shows the available integration types and, separately, the project's configured integrations

#### Scenario: No configured integrations

- **WHEN** the project has no configured integrations but available types exist
- **THEN** the gateway shows the available types with no configured entries and offers configuration

### Requirement: Configure a general integration

The gateway SHALL let the user create and update a named general integration with its settings. It MUST forward settings to Memory for storage and MUST NOT persist them in the gateway.

#### Scenario: Create an integration

- **WHEN** a signed-in user creates an integration with a name and settings
- **THEN** the gateway sends the configuration to Memory and the integration appears in the configured list

#### Scenario: Update an integration

- **WHEN** a signed-in user updates an existing integration's settings or enabled state
- **THEN** the gateway sends the update to Memory and reflects the new state

#### Scenario: Duplicate integration rejected

- **WHEN** a signed-in user creates an integration whose name already exists for the project
- **THEN** the gateway surfaces Memory's conflict to the user and does not overwrite the existing integration

### Requirement: Test an integration

The gateway SHALL let the user test an integration's credentials and connectivity without saving changes.

#### Scenario: Test succeeds

- **WHEN** a signed-in user tests a configured integration and Memory confirms the connection
- **THEN** the gateway reports success

#### Scenario: Test fails

- **WHEN** a signed-in user tests a configured integration and Memory reports the connection is invalid
- **THEN** the gateway reports the failure without altering the stored configuration

### Requirement: Sync an integration

The gateway SHALL let the user trigger a manual sync of an integration.

#### Scenario: Sync triggered

- **WHEN** a signed-in user triggers a sync on a configured integration
- **THEN** the gateway initiates the sync on Memory and confirms it started

#### Scenario: Sync not supported

- **WHEN** a signed-in user triggers a sync on an integration that does not support import
- **THEN** the gateway surfaces Memory's rejection to the user

### Requirement: Delete an integration

The gateway SHALL let the user delete a configured integration and its associated data.

#### Scenario: Delete

- **WHEN** a signed-in user deletes a configured integration
- **THEN** the gateway removes it from Memory and it disappears from the configured list

### Requirement: Never expose integration credentials

The gateway MUST NOT return, log, or persist integration settings or credentials; it SHALL rely on Memory's encrypted-at-rest storage and the fact that settings are omitted from API responses.

#### Scenario: Credentials absent from responses

- **WHEN** the gateway lists or reads an integration
- **THEN** no credential or secret value is present in the response

### Requirement: Project scoping and authentication

All integration views and actions SHALL be scoped to the active project using the project headers established by the session model, and SHALL require an authenticated session.

#### Scenario: Scoped to active project

- **WHEN** a signed-in user switches the active project
- **THEN** the integrations screen and all integration actions operate on the newly selected project

#### Scenario: Unauthenticated access

- **WHEN** a client accesses an integration view or action without a valid session
- **THEN** the gateway requires sign-in and performs no integration action
