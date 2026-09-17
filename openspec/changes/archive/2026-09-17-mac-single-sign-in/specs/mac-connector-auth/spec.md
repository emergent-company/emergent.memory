## MODIFIED Requirements

### Requirement: Sign in with Zitadel (native OAuth, PKCE)

The app SHALL sign users in through the selected environment's Zitadel OIDC
provider — using that environment's issuer and native client id — via the
authorization-code flow with PKCE (S256) in the system browser, and SHALL return
to the app via the registered custom-scheme redirect. A successful sign-in SHALL
create or select a per-account session for that environment (an account entry)
rather than a single app-wide session. Production SHALL be the default and the
only target offered by the primary signed-out surfaces; the Development
environment SHALL remain selectable, but only from the About page (see the
`mac-connector-app` capability for the surface rules).

#### Scenario: Successful sign-in

- **WHEN** the user starts sign-in for a selected environment with a valid issuer and native client id
- **THEN** the browser opens that environment's Zitadel login, and on success the app receives the authorization code, exchanges it (with the PKCE verifier) for tokens, persists the session for that account, and reports the user as signed in

#### Scenario: Sign in to another environment

- **WHEN** the user signs in to a different environment's issuer with that environment's client id
- **THEN** a new account entry is created for that environment without disturbing accounts already signed in

#### Scenario: Default sign-in targets Production

- **WHEN** the user starts sign-in from the window header, the menu-bar popover,
  the Connection page, or an add-account action
- **THEN** the request goes to the Production environment's issuer and client id,
  with no environment selection step

#### Scenario: Development sign-in starts from About

- **WHEN** the user starts the Development sign-in offered on the About page
- **THEN** the request goes to the Development environment's issuer and client id
  and follows the same PKCE flow and account-entry rules as Production

#### Scenario: Sign-in cancelled or fails

- **WHEN** the user cancels the login or the provider returns an error
- **THEN** the app stays signed out and shows a clear, non-fatal message

#### Scenario: Bad configuration

- **WHEN** the issuer is unreachable or the client id/redirect is rejected
- **THEN** the app reports the specific configuration error and suggests the required Zitadel app settings

## ADDED Requirements

### Requirement: Production is the primary environment

The app SHALL treat the Production environment as the primary sign-in target.
Surfaces that present "the" sign-in action SHALL resolve it to Production, and
SHALL NOT infer an alternative target from a user-facing environment control,
because no primary surface exposes one. A per-surface policy SHALL determine
which environments a given surface may offer, and it SHALL be unit-tested.

#### Scenario: Primary target is Production

- **WHEN** the app resolves the environment for a primary sign-in action
- **THEN** the resolved environment is Production

#### Scenario: Policy is the single source of truth

- **WHEN** the per-surface environment policy is queried for the About page
- **THEN** it returns both Production and Development, and for every other
  surface it returns Production only

#### Scenario: The Development environment still exists

- **WHEN** the environment list is read
- **THEN** it still contains both Production and Development, so Dev sign-in and
  existing Dev accounts continue to work — the policy restricts which surfaces
  offer Development, it does not remove the environment
