## Purpose

Lets a user sign in to a Memory instance with their Zitadel identity from the
macOS app, see who they are, switch between the projects they belong to, and
keep the local connector relaying to the selected project through a minted
project-scoped token — without pasting tokens by hand.

## ADDED Requirements

### Requirement: Sign in with Zitadel (native OAuth, PKCE)

The app SHALL sign users in through the instance's Zitadel OIDC provider using
the authorization-code flow with PKCE (S256) in the system browser, and SHALL
return to the app via a registered custom-scheme redirect.

#### Scenario: Successful sign-in

- **WHEN** the user starts sign-in with a valid issuer and native client id
- **THEN** the browser opens the Zitadel login, and on success the app receives the authorization code, exchanges it (with the PKCE verifier) for tokens, and reports the user as signed in

#### Scenario: Sign-in cancelled or fails

- **WHEN** the user cancels the login or the provider returns an error
- **THEN** the app stays signed out and shows a clear, non-fatal message

#### Scenario: Bad configuration

- **WHEN** the issuer is unreachable or the client id/redirect is rejected
- **THEN** the app reports the specific configuration error and suggests the required Zitadel app settings

### Requirement: Manage the OIDC session and rotating refresh tokens

The app SHALL store OIDC tokens in the Keychain, refresh the access token before
expiry or on a 401, serialize refreshes so a single-use refresh token is never
used twice concurrently, and persist a rotated refresh token before using the
new access token.

#### Scenario: Silent refresh

- **WHEN** the access token expires while the app is running
- **THEN** the app refreshes it and continues without user interaction

#### Scenario: Rotation is serialized

- **WHEN** two requests need a refreshed token at the same time
- **THEN** exactly one refresh runs and both requests use its result

#### Scenario: Refresh cannot complete

- **WHEN** a refresh fails (revoked/expired/network)
- **THEN** the app clears the session, returns to the signed-out state, and prompts sign-in again instead of failing silently

### Requirement: Sign out

The app SHALL sign the user out by revoking/expiring the session and clearing
stored tokens, and SHALL leave the app in a usable signed-out state.

#### Scenario: Sign out

- **WHEN** the user signs out
- **THEN** tokens are revoked/cleared from the Keychain, the connector token for the active project is cleared, and the UI shows the signed-out state

### Requirement: Configure the OIDC provider

The app SHALL let the user set the issuer URL and native client id (with sensible
defaults), validate them against the provider's discovery document, and make the
callback scheme discoverable to the provider.

#### Scenario: Validate provider configuration

- **WHEN** the user enters an issuer URL
- **THEN** the app checks `.well-known/openid-configuration` and reports whether the provider is usable

#### Scenario: Callback registration

- **WHEN** the app is installed
- **THEN** its custom callback scheme is declared so the browser can return the authorization code to the app

### Requirement: List and switch projects

The app SHALL list the Memory projects the signed-in user belongs to, let the
user select one, scope Memory calls to it via the project context, and remember
the selection.

#### Scenario: List projects

- **WHEN** the user is signed in
- **THEN** the project switcher lists the user's projects by name (identifiers available via the copy control)

#### Scenario: Switch project

- **WHEN** the user selects a different project
- **THEN** subsequent Memory calls use that project's context and the active selection is shown and persisted

### Requirement: Mint and use a project connector token

The app SHALL obtain a project-scoped connector token for the selected project
(reusing an existing one when possible), store it in the Keychain, and configure
the engine to relay with it so the connector works headlessly and independently
of the browser session.

#### Scenario: Token minted for the engine

- **WHEN** a project is selected and no connector token is stored for it
- **THEN** the app creates a project API token via the Memory API, stores it, writes it to the engine config, and restarts the engine

#### Scenario: Reuse on return

- **WHEN** the user returns to a previously used project with a stored token
- **THEN** the app reuses the stored token instead of minting another

#### Scenario: Signed out but engine running

- **WHEN** the user signs out while the engine is running
- **THEN** the engine is stopped and the stored connector token for the active project is cleared

### Requirement: Token-only fallback

The app SHALL continue to support configuring the connector with a manually
provided project token when OAuth is not used or unavailable.

#### Scenario: Manual token without sign-in

- **WHEN** the user provides a project token manually and does not sign in
- **THEN** the connector works as before, with identity shown as unavailable

### Requirement: Configure the connector per project

The app SHALL keep a per-project connector profile (which local MCP tools are
enabled, and the connector instance id) and SHALL apply the active project's
profile to the engine when that project is selected; account/server/provider
settings remain shared.

#### Scenario: Per-project profile applied on switch

- **WHEN** the user selects a project with a stored profile
- **THEN** the engine is configured with that project's enabled tools and instance id, and restarted

#### Scenario: Editing applies to the active project

- **WHEN** the user changes enabled tools or the instance id for the active project
- **THEN** the change is stored in that project's profile, written to the engine config, and the engine restarts

#### Scenario: No project selected

- **WHEN** no project is active (token-only setup)
- **THEN** the shared/default values are used and editing them behaves as before

### Requirement: Switch projects from the window header

The app SHALL expose the project switcher at the top of the window so the
active project can be changed from anywhere in the app.

#### Scenario: Switcher available at the top

- **WHEN** the main window is visible and the user is signed in
- **THEN** the window header (toolbar) shows the active project and lets the user switch to another project

