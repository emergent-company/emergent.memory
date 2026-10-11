## ADDED Requirements

### Requirement: Connect UI is provider-agnostic

The Data Sources connect surface SHALL list the available providers and render the connect control
for a provider from its advertised auth method and declared schemas, without a hard-coded list of
provider-specific pages.

#### Scenario: Provider chooser lists OAuth and credential providers

- **WHEN** the user opens the new-data-source page
- **THEN** every available provider is offered, with an OAuth provider shown as a "Connect with …" action and a credential provider shown as a credential form

### Requirement: OAuth providers connect without pasted secrets

For an OAuth provider the user SHALL start the connect from the UI and be redirected to the provider
to authorize; on return the UI SHALL reflect the connected source (or a clear error). The user SHALL
never be asked to paste a refresh token.

#### Scenario: Connect button starts the OAuth flow

- **WHEN** the user clicks "Connect with Google Drive"
- **THEN** the browser is sent to the provider's authorization page via the server authorize endpoint

#### Scenario: Callback returns to the UI with an outcome

- **WHEN** the provider redirects back after authorization
- **THEN** the UI shows the connected source on success, or an actionable error on failure, with no credential material exposed

### Requirement: Org admins manage provider OAuth clients

The UI SHALL let an organization admin set and update the OAuth client id/secret for a provider, and
SHALL never render the stored secret back.

#### Scenario: Admin sets a provider client

- **WHEN** an admin saves an OAuth client for a provider
- **THEN** it is stored (secret write-only) and subsequent connects for that org use it

#### Scenario: Stored secret is not echoed

- **WHEN** the settings page is loaded after a client is saved
- **THEN** the secret field is blank

### Requirement: Resource selection follows consent

After authorization the UI SHALL let the user select the resource the source syncs — a Drive folder
(or a folder from a listed tree) or a GitHub repository — before the source is finalized.

#### Scenario: Drive folder is selected after connect

- **WHEN** a Drive connect completes authorization
- **THEN** the user selects a folder (via the Picker for file-scoped access) and the chosen folder id is stored on the source

#### Scenario: GitHub repository is selected after connect

- **WHEN** a GitHub connect completes authorization
- **THEN** the user selects a repository (and optional path/ref) and it is stored on the source

### Requirement: Credential-based providers keep a manual form

A provider whose auth method includes `credentials` SHALL still be connectable by supplying
credential fields directly, and those fields SHALL be write-only.

#### Scenario: Manual credential connect still works

- **WHEN** the user connects a credentials-only provider
- **THEN** the credential fields are submitted, encrypted, and never echoed back
