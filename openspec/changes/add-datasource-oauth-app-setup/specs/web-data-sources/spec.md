## ADDED Requirements

### Requirement: Project admins manage project-scoped OAuth clients

The project settings SHALL expose a "Data source OAuth clients" page that lets a `project_admin`
set and update the OAuth client id/secret for a provider scoped to that project, and SHALL never
render the stored secret back. The page SHALL reuse the same OAuth-client card as the org settings
page, and a project-scoped client SHALL override the org client and deployment default for connects
started in that project.

#### Scenario: Project admin reaches the project OAuth clients page

- **WHEN** a project admin follows the "no OAuth client configured" remediation link on a failed OAuth connect
- **THEN** it opens the active project's OAuth-client settings page (not the org page)

#### Scenario: Non-admin cannot access project OAuth clients

- **WHEN** a user who is not `project_admin` of the project opens the project OAuth-client settings
- **THEN** the page is not shown and the endpoint is not reachable (404)

#### Scenario: Project admin saves a client

- **WHEN** a project admin saves an OAuth client for a provider on the project page
- **THEN** it is stored (secret write-only) and subsequent connects in that project use it over the org client and deployment default

#### Scenario: Stored secret is not echoed

- **WHEN** the project OAuth-client page is loaded after a client is saved
- **THEN** the secret field is blank

### Requirement: OAuth-client card is scope-agnostic with a Google Cloud Console guide

The org and project settings pages SHALL render the same scope-agnostic OAuth-client card, which SHALL
collect the client id, secret (write-only), scopes, and strategy, SHALL display the deployment-wide
`redirectUri` (provider callback) and `javascriptOrigin` identically on both pages, and SHALL include a
Google Cloud Console setup guide telling the user the exact redirect URI and origin to register on the
Google app.

#### Scenario: Org and project pages share the same card

- **WHEN** an org admin and a project admin each open their OAuth-client settings for the same provider
- **THEN** both render the same card with the same fields and validation

#### Scenario: Redirect URI and origin are shown identically

- **WHEN** either settings page is loaded
- **THEN** the deployment-wide `redirectUri` and `javascriptOrigin` are displayed with the values to register in Google Cloud Console

#### Scenario: Console guide names the values to register

- **WHEN** the card is rendered for Google Drive
- **THEN** the guide states the redirect URI (and origin where applicable) to enter as the app's authorized redirect URI / JavaScript origin

### Requirement: Missing-client alert points at the right settings

When a connect fails because no OAuth client is configured for the provider, the UI SHALL show an
actionable alert that links to the project's OAuth-client settings when the caller can administer the
active project, and otherwise to the org's OAuth-client settings.

#### Scenario: Project admin is pointed at project settings

- **WHEN** a `project_admin` of the active project hits the missing-client error
- **THEN** the alert links to the project's "Data source OAuth clients" settings

#### Scenario: Non-admin is pointed at org settings

- **WHEN** a user who cannot administer the project hits the missing-client error
- **THEN** the alert links to the org's "Data source OAuth clients" settings

## MODIFIED Requirements

### Requirement: Org admins manage provider OAuth clients

The UI SHALL let an organization admin set and update the OAuth client id/secret for a provider using
the shared scope-agnostic OAuth-client card, SHALL never render the stored secret back, and SHALL
display the deployment-wide `redirectUri` and `javascriptOrigin` with the Google Cloud Console setup
guide.

#### Scenario: Admin sets a provider client

- **WHEN** an admin saves an OAuth client for a provider
- **THEN** it is stored (secret write-only) and subsequent connects for that org use it when no project client overrides it

#### Scenario: Stored secret is not echoed

- **WHEN** the settings page is loaded after a client is saved
- **THEN** the secret field is blank
