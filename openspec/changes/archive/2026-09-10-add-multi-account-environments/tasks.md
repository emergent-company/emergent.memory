# Tasks — add-multi-account-environments

TDD with the existing stub/secret-store test patterns. Model/auth core first
(testable on Linux), then UI (designer), then the Mac gate.

## 1. Environment model

- [ ] 1.1 `Environment` value type + built-ins: prod (`https://memory.emergent-company.ai`,
  issuer `https://auth.emergent-company.ai`, client `390138006318678019`) and
  dev (`https://api.dev.emergent-company.ai`, issuer
  `https://zitadel.dev.emergent-company.ai`, client `390138928478289930`), with
  `id/name/serverURL/issuer/clientID` + `all`. Unit tests (constants, ids).
- [ ] 1.2 Route sign-in/API config through `Environment` (issuer + client id +
  server URL) instead of the single ConnectorSettings fields; keep those fields
  as the active environment's effective values (no behaviour change for prod).

## 2. Accounts + per-account storage

- [ ] 2.1 `Account` model + non-secret `accounts.json` index + `activeAccountID`
  persistence (UserDefaults). Tests: add/remove/list/active persistence.
- [ ] 2.2 Per-account secret directories (`~/.config/memory-connector/accounts/<id>/…`)
  via `AppSecretStore(baseDirectory:)`; `AccountStore` loads/saves each
  account's session independently. Tests: isolation (two accounts, distinct
  sessions/tokens), corrupt/missing tolerance.
- [ ] 2.3 `AccountStore.signIn(environment:)` (PKCE against env issuer/client,
  create/lookup the account, persist session, make active) and
  `switchTo(accountID:)` (disconnect engine → set active → reload projects +
  identity) and `signOut(accountID:)` (disconnect if active; delete that
  account's secrets + index entry; others untouched). Tests (stubbed OIDC +
  injectable engine hooks).
- [ ] 2.4 Per-account refresh (one RefreshCoordinator per account; a failure
  marks only that account as needing re-auth, never clearing others). Tests.
- [ ] 2.5 One-time migration of the legacy single session + secrets into a prod
  account; non-destructive ordering. Tests (legacy present/absent, partial).
  Verify: manual smoke after install keeps the user signed in.

## 3. Project/engine isolation per account

- [ ] 3.1 Account-scoped `ProjectStore` state (profiles, project tokens,
  connected project, active project) under the active account; switching
  accounts clears/reloads it and keeps the previous account's data on disk.
  Tests.
- [ ] 3.2 Token naming includes env/account (`Memory Connector (macOS) – <instance> – <env>`)
  to avoid cross-env collisions. Tests.
- [ ] 3.3 Engine config materialised from the active account's project; account
  switch disconnects (engine stopped) and only the connected active account
  runs. Tests + reuse `EngineLifecyclePolicy`.

## 4. UI (designer)

- [ ] 4.1 Profile icon menu: list signed-in accounts (initials avatar, email,
  environment badge, active checkmark) → switch; "Add account ▸ Prod / Dev";
  "Manage accounts…". Signed-out → sign-in with environment choice (Prod / Dev).
- [ ] 4.2 Account page: accounts list with per-account sign-out + "Add account";
  show environment per account.
- [ ] 4.3 Menu-bar popover reflects the active account + env (and switches).
- [ ] 4.4 Manual smoke: add a Prod and a Dev account; switch; verify only the
  active account's projects/orgs/connection appear and the connector disconnects
  on switch.

## 5. Gate

- [ ] 5.1 Hosted tests green; `tools/mac-build.sh --test` passes.
- [ ] 5.2 Install + manual smoke incl. dev sign-in (needs the dev Zitadel app to
  allow `com.emergent.memory.connector://callback`); record a session doc.
