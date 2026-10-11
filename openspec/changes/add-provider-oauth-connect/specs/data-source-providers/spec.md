## ADDED Requirements

### Requirement: Providers are registered behind one interface

The server SHALL register data-source providers through a provider interface and resolve them by
`providerType`; the CRUD, sync, and HTTP layers SHALL NOT branch on a concrete provider name.
Adding a provider SHALL require only a new provider package and its registration.

#### Scenario: Catalog lists every registered provider

- **WHEN** a client lists data-source providers
- **THEN** every registered provider is returned with its type, name, description, source type, and availability

#### Scenario: Unknown provider is not found

- **WHEN** a client requests the config schema for an unregistered provider type
- **THEN** the server responds not-found rather than a hard-coded error

#### Scenario: Core is provider-agnostic

- **WHEN** a provider is registered
- **THEN** no change to the sync worker, CRUD service, or route table is required to serve it

### Requirement: Catalog advertises the provider auth method

Each provider SHALL advertise its supported authentication method (`oauth`, `credentials`, or
`both`), and the connection-test and connect surfaces SHALL be driven by it.

#### Scenario: OAuth provider advertises oauth

- **WHEN** a client reads the provider catalog
- **THEN** an OAuth-based provider reports `oauth` and a credential-based provider reports `credentials`

### Requirement: OAuth connect uses authorize and callback endpoints

The server SHALL expose a provider-scoped OAuth authorize endpoint and a callback endpoint. The
authorize endpoint SHALL return an authorization URL containing a single-use `state` and a PKCE
(`S256`) challenge. The callback SHALL verify the state, exchange the authorization code using the
client secret and PKCE verifier, and store the resulting tokens encrypted on the source.

#### Scenario: Authorize builds a stateful PKCE URL

- **WHEN** an authenticated user starts a connect for an OAuth provider
- **THEN** the server returns an authorization URL with a `state` and an `S256` code challenge and records the pending state bound to the project, provider, and initiating user

#### Scenario: Callback exchanges the code and stores tokens

- **WHEN** the provider redirects back with a valid `code` and matching `state`
- **THEN** the server exchanges the code for tokens and persists them encrypted on the source, and no credential material is returned to the browser

#### Scenario: Invalid, expired, or reused state is rejected

- **WHEN** the callback receives a missing, expired, or already-consumed `state`
- **THEN** the server stores no token and returns an error

### Requirement: OAuth client credentials are org-scoped and write-only

OAuth client id/secret SHALL be resolvable per organization and per provider, stored encrypted, and
never returned in any response. When no org config exists, the server SHALL fall back to a
deployment-level default and otherwise report a configuration error.

#### Scenario: Org client overrides deployment default

- **WHEN** an org has configured a client for a provider and a deployment default also exists
- **THEN** the org's client is used

#### Scenario: Missing client is a clear error

- **WHEN** neither an org client nor a deployment default exists for a provider
- **THEN** starting a connect fails with an actionable configuration error

#### Scenario: Client secret is write-only

- **WHEN** an OAuth client config is created and then read back
- **THEN** the response never contains the client secret

### Requirement: Provider-generic credential and token model

The stored credential SHALL be a provider-defined opaque encrypted blob; the provider SHALL obtain a
usable access token at sync and test time. The core SHALL NOT assume a refresh token or any specific
credential field.

#### Scenario: Refresh-token provider renews server-side

- **WHEN** a provider whose token expires is used after expiry
- **THEN** its connector obtains a fresh access token from the stored credential without user interaction

#### Scenario: Static-token provider needs no renewal

- **WHEN** a provider issues a long-lived token
- **THEN** its connector uses the stored credential directly and the core performs no refresh

### Requirement: Connection test is provider-generic

Testing a provider's credentials or stored source SHALL go through the provider interface and return
a success/failure message, without the core knowing the provider's protocol.

#### Scenario: Test uses the stored source credential

- **WHEN** a client tests an existing source's connection
- **THEN** the server decrypts the stored credential, performs a bounded provider call, and returns the outcome

### Requirement: Second provider can be added without core changes

The framework SHALL support at least a second provider (GitHub) via registration only, so that
Drive and GitHub are served by the same connect, test, and sync contracts.

#### Scenario: GitHub is registered as a provider

- **WHEN** the GitHub provider package is registered
- **THEN** it appears in the catalog with its own config/credential schema, auth method, and connector, served by the unchanged core
