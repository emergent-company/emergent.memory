## MODIFIED Requirements

### Requirement: OAuth client credentials are org- and project-scoped and write-only

OAuth client id/secret SHALL be resolvable per project, per organization, and per provider, stored
encrypted, and never returned in any response. When starting a connect the server SHALL resolve the
client in the order **project config → org config → deployment env default**, and otherwise report an
actionable configuration error. Setting a project or org client SHALL require the narrow authority for
that scope — `project_admin` of that project for project endpoints, `org_admin` for org endpoints — and
a scope SHALL NOT read or mutate another scope's client.

#### Scenario: Project client overrides org and deployment default

- **WHEN** a project has configured a client for a provider, and both an org client and a deployment default also exist
- **THEN** the project's client is used for that project's connects

#### Scenario: Org client is the fallback beneath project

- **WHEN** a project has no client for a provider but its org does
- **THEN** the org's client is used

#### Scenario: Deployment default is the last fallback

- **WHEN** neither a project nor an org client exists for a provider but a deployment env default does
- **THEN** the deployment default is used

#### Scenario: Missing client is a clear error

- **WHEN** neither a project client, an org client, nor a deployment default exists for a provider
- **THEN** starting a connect fails with an actionable configuration error naming the provider and the remediation location

#### Scenario: Client secret is write-only

- **WHEN** an OAuth client config is created and then read back
- **THEN** the response never contains the client secret

#### Scenario: Project endpoints require project admin

- **WHEN** a caller that is not `project_admin` of the target project invokes a project OAuth-client endpoint
- **THEN** the server rejects the request and reveals no client material

#### Scenario: A scope cannot read or mutate another scope's client

- **WHEN** a project admin requests or mutates another project's client, or an org client through the project endpoint
- **THEN** the server returns not-found or forbidden and never exposes or changes the other scope's client

## ADDED Requirements

### Requirement: Google Drive is OAuth-only

Google Drive SHALL advertise `AuthMethods` of `[oauth]` only, and the paste-refresh-token
(credentials) connect path for Drive SHALL be removed. Existing Drive sources whose stored credential
is a refresh token SHALL continue to sync — the stored token is still honoured server-side.

#### Scenario: Drive advertises only OAuth

- **WHEN** a client reads the provider catalog
- **THEN** `google_drive` reports `oauth` and no `credentials` method

#### Scenario: New Drive connects cannot paste a refresh token

- **WHEN** a user connects a Drive source
- **THEN** the only available path is the OAuth connect flow, and no refresh-token field is offered

#### Scenario: Existing pasted sources keep syncing

- **WHEN** a pre-existing Drive source holds a stored refresh token
- **THEN** its syncs continue to work and renew using the stored token without re-authorization
