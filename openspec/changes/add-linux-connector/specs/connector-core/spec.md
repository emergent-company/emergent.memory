## Purpose

Provides the OS-agnostic connector core — user sign-in and session management, project selection and project-scoped token issuance, local secret storage, engine configuration, supervision policy, and a machine-readable status model — so the macOS app and the Linux CLI/daemon share one implementation.

## ADDED Requirements

### Requirement: Sign in with OIDC Authorization Code + PKCE

The core SHALL authenticate a user through an environment's OIDC provider using the Authorization Code flow with PKCE (S256), opening the system browser and receiving the callback through an environment-appropriate redirect, and SHALL validate the issuer against the provider's discovery document. A successful sign-in SHALL create or select a per-account session for that environment rather than a single global session.

#### Scenario: Successful sign-in
- **WHEN** the user starts sign-in for an environment with a reachable issuer and client id
- **THEN** the browser opens that environment's login, and on success the core exchanges the code (with the PKCE verifier) for tokens and reports the user as signed in for that account

#### Scenario: Sign-in cancelled or provider error
- **WHEN** the user cancels login or the provider returns an error
- **THEN** the core stays signed out and reports a clear, non-fatal message

#### Scenario: Unusable provider configuration
- **WHEN** the issuer is unreachable or the client id/redirect is rejected
- **THEN** the core reports the specific configuration error and suggests the required provider settings

### Requirement: Sign in headlessly with the OIDC device authorization flow

The core SHALL support signing in without a browser redirect by presenting a device user code and verification URI, polling the token endpoint until authorization completes or expires, and persisting the resulting session. This path SHALL require no redirect URI and SHALL reuse the OAuth client already registered by the Memory platform for its CLI, so no new provider application is required.

#### Scenario: Device code presented
- **WHEN** the user starts headless sign-in
- **THEN** the core requests a device code and displays the user code and verification URI

#### Scenario: Authorization completes
- **WHEN** the user authorizes the device code at the verification URI
- **THEN** the core obtains tokens, persists the session, and reports the user as signed in

#### Scenario: Session is refreshable
- **WHEN** headless sign-in completes
- **THEN** the persisted session includes a refresh token so the core can renew access later without re-authentication

#### Scenario: Authorization expires or is denied
- **WHEN** the device code expires or the user denies authorization
- **THEN** the core stops polling and reports a clear, non-fatal failure without persisting a session

### Requirement: Two-step PKCE login for GUI front-ends

The core SHALL support an interactive PKCE sign-in as two steps, so a graphical front-end can own the browser and custom-scheme callback while the core owns PKCE, token exchange, and storage: a start step returns an authorization URL, an opaque login identifier, and the state; a complete step accepts the authorization result and finishes sign-in. The core SHALL keep the PKCE verifier server-side and SHALL NOT expose it.

#### Scenario: Start returns an authorization URL
- **WHEN** a front-end starts PKCE sign-in with a redirect URI
- **THEN** the core returns an authorization URL containing only the PKCE challenge, plus a login identifier and a state value

#### Scenario: Complete finishes sign-in
- **WHEN** the front-end submits the authorization code and matching state for a login identifier
- **THEN** the core validates the state, exchanges the code with the stored verifier, persists the session, and returns the signed-in identity

#### Scenario: State mismatch is rejected
- **WHEN** the submitted state does not match the stored value
- **THEN** the core rejects the attempt with a distinguishable state-mismatch error and does not persist a session

#### Scenario: Unknown or expired login is rejected
- **WHEN** the login identifier is unknown or its pending record has expired
- **THEN** the core rejects the attempt with a distinguishable error and does not exchange the code

#### Scenario: Concurrent logins are isolated
- **WHEN** two PKCE sign-ins are started before either completes
- **THEN** each login identifier completes or fails independently without cross-contaminating state or verifiers

### Requirement: Reuse the Memory platform authentication and API client

The core SHALL reuse the Memory platform's authentication components (OIDC device flow, discovery, refresh, revocation, and 0600 credential store) rather than maintaining a divergent implementation, and SHALL consume the published Memory core Go SDK for project and token API access so request/response behavior tracks the platform.

#### Scenario: Authentication components are shared
- **WHEN** the core performs discovery, refresh, or revocation
- **THEN** it uses the same platform-provided authentication components the Memory CLI uses

#### Scenario: Project and token APIs use the core SDK
- **WHEN** the core lists projects or mints/revokes a project token
- **THEN** it calls the Memory core Go SDK rather than a private reimplementation of those endpoints

### Requirement: Interoperate with the Memory CLI credential store

The core SHALL be able to import an existing Memory CLI OIDC session when it has no session of its own, and SHALL NOT modify the Memory CLI's credential file.

#### Scenario: Import an existing CLI session
- **WHEN** the core starts with no session of its own and a Memory CLI session exists
- **THEN** the core can adopt that session instead of requiring a fresh sign-in

#### Scenario: The CLI store is not mutated
- **WHEN** the core signs in, refreshes, or signs out using its own store
- **THEN** the Memory CLI credential file is left unchanged

### Requirement: Maintain a rotating OIDC session

The core SHALL refresh the access token before expiry or on a 401, SHALL serialize refreshes so a single-use refresh token is never used twice concurrently, SHALL persist a rotated refresh token before using the new access token, and SHALL clear the session and require sign-in again when a refresh cannot complete.

#### Scenario: Silent refresh
- **WHEN** the access token expires while the process is running
- **THEN** the core refreshes it and continues without user interaction

#### Scenario: Rotation is serialized
- **WHEN** two operations need a refreshed token at the same time
- **THEN** exactly one refresh runs and both operations use its result

#### Scenario: Refresh cannot complete
- **WHEN** a refresh fails because the token was revoked, expired, or the network is unavailable
- **THEN** the core clears the session and reports the signed-out state instead of failing silently

### Requirement: Sign out

The core SHALL sign the user out by revoking or expiring the session and clearing stored tokens, and SHALL clear the connector token for the active project, leaving a usable signed-out state.

#### Scenario: Sign out clears credentials
- **WHEN** the user signs out
- **THEN** the stored session and the active project's connector token are cleared and the signed-out state is reported

### Requirement: Manage accounts and projects

The core SHALL maintain one session per environment, SHALL list the projects the signed-in user belongs to (by name, with raw identifiers available for copying), SHALL let the user select an active project, and SHALL persist the active selection across restarts.

#### Scenario: List projects
- **WHEN** the user is signed in and lists projects
- **THEN** the core returns the user's projects with names and identifiers

#### Scenario: Switch active project
- **WHEN** the user selects a different project
- **THEN** subsequent operations use that project's context and the selection is persisted

#### Scenario: Multiple environments
- **WHEN** the user signs in to a second environment
- **THEN** a new account entry is created without disturbing sessions already established

### Requirement: Mint, reuse, and revoke project connector tokens

The core SHALL obtain a project-scoped connector token for the active project, SHALL reuse a stored token for that project when one exists, SHALL store it securely, and SHALL clear it on sign-out so the relay is independent of the browser session.

#### Scenario: Token minted for the relay
- **WHEN** a project is selected and no connector token is stored for it
- **THEN** the core creates a project token through the Memory API, stores it, and makes it available to the engine configuration

#### Scenario: Reuse on return
- **WHEN** the user returns to a previously used project with a stored token
- **THEN** the core reuses the stored token instead of minting another

#### Scenario: Token cleared on sign-out
- **WHEN** the user signs out while a project is active
- **THEN** the stored connector token for that project is cleared

#### Scenario: Least-privilege token scope
- **WHEN** the core mints a connector token
- **THEN** it requests the least privilege the relay needs (read-only data access) rather than a broad or administrative scope

### Requirement: Store secrets on local disk

The core SHALL store sessions, project tokens, and manual tokens in files with mode 0600 inside a directory with mode 0700, using atomic writes, SHALL be identity-independent so secrets persist across rebuilds and upgrades, and SHALL NOT require an OS keychain.

#### Scenario: Restrictive permissions
- **WHEN** the core writes a secret file
- **THEN** the file mode is 0600 and its parent directory mode is 0700

#### Scenario: Persist across upgrade
- **WHEN** the binary is rebuilt or upgraded
- **THEN** stored secrets remain readable and no re-authentication is required

#### Scenario: Tolerant reads
- **WHEN** a secret file is missing or partially written
- **THEN** the core treats the secret as absent and does not fail the surrounding operation

### Requirement: Materialize engine configuration

The core SHALL write the engine configuration (server URL, token, project id, instance id, disabled tools) with a 0600 file inside a 0700 directory, SHALL treat that file as the only plaintext location for the token, SHALL refresh it before each relay start, and SHALL import an existing CLI-created configuration.

#### Scenario: Materialize before start
- **WHEN** a relay start is requested for the active profile
- **THEN** the config file is written from the active profile and token immediately before the engine starts

#### Scenario: Import existing configuration
- **WHEN** a config file already exists that was created by the CLI
- **THEN** the core reads its values (server, token, instance id, disabled tools) into the active state without requiring re-entry

### Requirement: Preserve configuration and path compatibility

The core SHALL continue to read and write the existing configuration format and SHALL resolve paths per platform: the established per-user config location on macOS, and XDG base directories on Linux. An explicit configuration-path override SHALL take precedence on every platform, and an existing configuration MUST NOT be invalidated or rewritten into a new location by an upgrade.

#### Scenario: Existing macOS configuration keeps working
- **WHEN** the core runs on macOS with a configuration at the established per-user location
- **THEN** it reads and writes that same file without requiring a migration step

#### Scenario: Explicit path override wins
- **WHEN** an explicit configuration path is supplied
- **THEN** the core reads and writes that path regardless of platform defaults

#### Scenario: Upgrade does not strand configuration
- **WHEN** an installed connector is upgraded to a shared-core release
- **THEN** the previously written configuration remains valid and the connector starts without rewriting it elsewhere

### Requirement: Token-only fallback

The core SHALL support configuring the connector with a manually provided project token when OIDC is not used or unavailable, and SHALL report user identity as unavailable in that mode.

#### Scenario: Manual token without sign-in
- **WHEN** the user provides a project token manually and does not sign in
- **THEN** the connector relays with that token and reports identity as unavailable

### Requirement: Report structured status

The core SHALL report local state — instance id, version, active project, the registered tool list, and hub presence — as human-readable text by default and as JSON when requested, SHALL classify hub state as connected, not connected, authentication failed, unreachable, or missing configuration, and SHALL exit non-zero only for configuration or usage errors.

#### Scenario: Human-readable status
- **WHEN** status is requested without a JSON flag
- **THEN** the core prints instance id, version, tool list, and a hub line describing the connection state

#### Scenario: Machine-readable status
- **WHEN** status is requested with the JSON flag
- **THEN** the core emits a stable JSON document containing the same fields and the hub state as an enumerated value

#### Scenario: Missing configuration
- **WHEN** no configuration exists
- **THEN** status exits non-zero with a message directing the user to configure the connector first

### Requirement: Stable CLI and JSON contract for front-ends

The core SHALL expose sign-in, sign-out, project listing/selection, configuration, relay, and status operations over a versioned command surface whose JSON output is stable, so the macOS GUI and Linux tooling consume the same interface. Machine-readable output SHALL carry a schema version so consumers can detect incompatible changes, and the core SHALL expose a version command. Exit codes SHALL be stable: zero for success, non-zero for configuration or runtime failure, and a distinct code for usage errors.

#### Scenario: GUI consumes the shared surface
- **WHEN** a graphical front-end needs account, project, or status data
- **THEN** it obtains it from the shared command surface without reimplementing core behavior

#### Scenario: Machine output is versioned
- **WHEN** a front-end requests machine-readable output
- **THEN** the document includes a schema version identifying the contract revision

#### Scenario: Version is discoverable
- **WHEN** the version command is invoked
- **THEN** the core prints its version without requiring configuration or network access

#### Scenario: Usage errors are distinguishable
- **WHEN** a command is invoked with invalid arguments
- **THEN** the core exits with the dedicated usage-error code and prints guidance, distinct from configuration or runtime failures

### Requirement: Shared supervision policy

The core SHALL provide a supervision policy that restarts a crashed engine with bounded attempts within a time window, transitions to a give-up state when the bound is exceeded, and suppresses restarts after a deliberate user stop, usable by an in-process supervisor on any platform.

#### Scenario: Restart within bound
- **WHEN** the engine exits unexpectedly and the restart bound is not exceeded
- **THEN** the policy schedules a restart after a delay

#### Scenario: Give up after repeated exits
- **WHEN** the engine exits more times than the bound within the window
- **THEN** the policy reports a give-up state and stops restarting

#### Scenario: Deliberate stop suppresses restart
- **WHEN** the user stops the engine deliberately
- **THEN** the policy does not restart it
