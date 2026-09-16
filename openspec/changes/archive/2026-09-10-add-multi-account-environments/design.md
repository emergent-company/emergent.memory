# Design — add-multi-account-environments

## Context

Today the app has a single OIDC session (`OIDCSessionStore`) and a single
server URL (`ConnectorSettings.serverURL`), with secrets in
`~/.config/memory-connector/*.json` (0600) and a Keychain-free design. The
engine runs at most one project (explicit connect toggle). Dev/prod values come
from the user:

- Prod — server `https://memory.emergent-company.ai`, issuer
  `https://auth.emergent-company.ai`, client `390138006318678019`.
- Dev — server `https://api.dev.emergent-company.ai` (verified reachable,
  401 without auth), issuer `https://zitadel.dev.emergent-company.ai`
  (discovery verified), client `390138928478289930`.

## Goals / Non-Goals

Goals: environments, multiple simultaneous accounts, per-account isolation,
account switcher from the profile icon, migration of the existing session.

Non-goals: simultaneous connections for several accounts (still one engine /
one connected project at a time), cross-account dashboards, custom environments
UI (only built-ins in v1), backend changes.

## Decisions

### 1. `Environment` as a value

```swift
struct Environment: Identifiable, Hashable {
  let id: String        // "prod" | "dev"
  let name: String      // "Memory" | "Memory Dev"
  let serverURL: URL
  let issuer: URL
  let clientID: String
}
static let prod / dev / all
```
Callback scheme stays `com.emergent.memory.connector://callback` for both
(prerequisite: the dev Zitadel app must allow it; flag to ops if sign-in fails).

### 2. Account identity + index

`struct Account: Identifiable { id: String; environmentID: String; email?; displayName?; avatarObjectKey? }`
with `id = "<envID>:<sub-or-email>"`. A non-secret index
`accounts.json` lists accounts; `activeAccountID` persists in UserDefaults.
Secrets move to **per-account directories**:

```
~/.config/memory-connector/
  accounts.json                     # index (no secrets)
  accounts/<accountID>/session.json # OIDC session (0600)
  accounts/<accountID>/projects-tokens.json
  accounts/<accountID>/manual-token
```
`AppSecretStore` gains an injected base directory (already injectable) so each
account gets its own store instance.

### 3. `AccountStore` replaces the single-session assumptions

`@MainActor final class AccountStore` (or AuthStore evolves in place):
- `accounts: [Account]`, `activeAccount: Account?`, `sessions: [String: OIDCSession]` (loaded on demand).
- `signIn(environment:)` → discovery(env) + PKCE(env.clientID) + exchange → create/lookup account entry → persist session in the account dir → make active.
- `switchTo(accountID:)` → **disconnect the engine** for the previous account, set active (persist), then load that account's projects + identity.
- `signOut(accountID:)` → disconnect if active, delete that account's secrets + index entry; keep others.
- `currentAccessToken(for:)` → per-account refresh via the existing RefreshCoordinator (one coordinator per account, so rotation can't leak across accounts).
- Refresh failures affect only that account (mark it needs-reauth; do not clear others).

### 4. Per-account isolation for projects/engine

- `ProjectStore` gains an **account scope**: profiles + project tokens live in the
  active account's directory; `connectedProjectID`, active project, and local
  tool profiles are per account.
- Minted connector token name includes account/env to avoid collisions across
  environments: e.g. `Memory Connector (macOS) – <instanceID> – <envID>`.
- Engine config is materialised from the **active account's** active project;
  `switchTo` stops the engine (engine runs iff the active account has a
  connected project). No two accounts can be connected at once.

### 5. Migration

On first launch: if `accounts.json` is absent and legacy
`~/.config/memory-connector/{session.json,projects-tokens.json,manual-token}`
exist, create a prod account (identity from the session/userinfo when
available), move the files into `accounts/<id>/`, and mark it active. If
identity is unknown, derive a stable id (`prod:legacy`) and let the first
successful API call fill in email/name.

### 6. UI

- Profile icon → `NSMenu`/SwiftUI menu listing accounts (initials avatar,
  email, env badge) with a checkmark on the active one, then
  "Add account ▸ Prod / Dev", and "Manage accounts…" → Account page.
- Account page: list of accounts with per-account sign-out and an "Add account"
  button; sign-in screen offers Prod/Dev (dev preset prefilled with the dev
  issuer/client/server).

## Risks / Trade-offs

- [Dev Zitadel app missing the callback scheme] → sign-in error; document + flag
  to ops (the issue template already covers client setup).
- [Migration data loss] → move (not copy-delete) only after a successful read;
  keep legacy files until the account session loads successfully.
- [Token/profile collisions across envs] → account-scoped directories + env in
  token names.
- [Refresh rotation across accounts] → one RefreshCoordinator per account,
  serialized per account.
- [Only one account connected at a time] → explicit by design; switching
  disconnects (documented in UI copy).

## Migration Plan

Additive with a one-time, non-destructive migration of the existing session.
Rollback = keep legacy files; the app can be rebuilt without the accounts layer
and still read them.

## Open Questions

- Whether to allow custom environments later (v2).
- Dev API base: verified `api.dev.emergent-company.ai`; confirm it is the
  canonical dev host for the connector (prod uses `memory.emergent-company.ai`).
