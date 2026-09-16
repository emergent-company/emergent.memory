## Why

The app supports a single signed-in account tied to one environment. Users need
to (a) be signed in to **several accounts at once** and switch between them, with
strict isolation of projects/organisations/tokens, and (b) sign in to a **dev
environment** with its own Memory server URL and Zitadel provider.

Environments (provided):
- Prod — server `https://memory.emergent-company.ai`, issuer
  `https://auth.emergent-company.ai`, client id `390138006318678019` (app "Memory Connector").
- Dev — server `https://api.dev.emergent-company.ai`, issuer
  `https://zitadel.dev.emergent-company.ai`, client id `390138928478289930` (app "Memory Connector Dev").

## What Changes

- **Environments**: a first-class `Environment` (name, server URL, issuer,
  client id) with built-in Prod and Dev; the sign-in flow takes an environment.
- **Multiple accounts**: the app keeps a list of signed-in accounts (each =
  environment + user). Sessions are stored per account; the **active account**
  drives identity, projects, organisations, the connected project, and local
  tool profiles. No data is shared between accounts.
- **Account switcher**: clicking the profile icon shows all signed-in accounts
  (avatar/initials, email, environment badge) — selecting one switches the
  active account; plus "Add account" / "Sign in to Dev". Account details and
  per-account sign-out live on the Account page.
- **Isolation & switching**: switching accounts disconnects the current project
  (engine stops) and loads only the new account's data; per-account project
  tokens, profiles, and instance ids; token names include the account/env to
  avoid collisions across environments.
- **Storage**: per-account 0600 secret directories; an account index
  (non-secret) + active-account id; one-time migration of the existing single
  session into an account entry.
- No backend/gateway changes; the engine/Go module is unchanged (it just gets a
  per-account config).

## Capabilities

### New Capabilities
- `multi-account-environments`: environments (Prod/Dev) and multiple signed-in
  accounts with per-account isolation and switching.

### Modified Capabilities
- `mac-connector-auth`: sign-in is parameterised by environment (server URL,
  issuer, native client id) and returns an account entry rather than the single
  app-wide session.

## Impact

- Swift sources under `client/macos/MemoryConnector/` (Environment model,
  AccountStore/AuthStore, ProjectStore isolation, secrets layout, UI).
- Secret files move to `~/.config/memory-connector/accounts/<accountId>/…`
  (0600) with migration; no Keychain use (unchanged).
- Prerequisite: the dev Zitadel app must allow the app's callback scheme
  (`com.emergent.memory.connector://callback`) — ops task if not already set.
- Tests: environment resolution, account add/switch/remove, isolation,
  migration, per-account token naming.
