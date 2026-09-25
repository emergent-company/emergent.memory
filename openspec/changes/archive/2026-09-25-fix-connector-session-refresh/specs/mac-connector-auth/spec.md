## ADDED Requirements

### Requirement: Preserve the session established by PKCE sign-in

The macOS app SHALL NOT re-import (and thereby overwrite) the connector CLI session it just
established through `auth complete`. The bridge that imports a session into the CLI SHALL
only run when the CLI reports no usable session (legacy or missing).

#### Scenario: Sign-in leaves the CLI session intact

- **WHEN** the app completes a PKCE sign-in and `auth complete` reports signed in
- **THEN** the app does not run `auth import`, so the CLI-stored session including its refresh token is unchanged

#### Scenario: Self-heal only for an absent session

- **WHEN** the CLI reports no valid session for the active account
- **THEN** the app may run the bridge to import a legacy/absent session
