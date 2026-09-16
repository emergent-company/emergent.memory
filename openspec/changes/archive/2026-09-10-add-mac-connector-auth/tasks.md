# Tasks — add-mac-connector-auth

Deterministic unit tests with the existing `StubURLProtocol` where HTTP is
involved; the browser OAuth round-trip and real project switching are manual
macOS smoke. Implementation in the existing worktree lane; commit per unit.

## 1. OIDC core (no UI)

- [x] 1.1 PKCE utilities (CryptoKit): random verifier (43–128 chars, unreserved),
  S256 challenge (base64url, no padding), random state/nonce; unit tests
  (challenge vector, charset/length, uniqueness).
- [x] 1.2 OIDC discovery + authorize/token client: fetch
  `{issuer}/.well-known/openid-configuration` (cache; error on missing
  endpoints), build the authorization URL (client_id, redirect_uri,
  response_type=code, scope `openid profile email offline_access`,
  code_challenge/method=S256, state, nonce), exchange code
  (`grant_type=authorization_code`, code_verifier, no client_secret), refresh
  (`grant_type=refresh_token`), revoke (`/oauth/v2/revoke`), end_session URL,
  userinfo. Tests with `StubURLProtocol` (success, 4xx/5xx, malformed JSON,
  missing discovery fields) + authorize-URL assertions (encoded params, PKCE).
- [x] 1.3 Token response model + expiry handling (expires_at = now +
  expires_in, leeway constant); tests.

## 2. Session store + refresh

- [x] 2.1 Keychain-backed `OIDCSessionStore`: access/refresh/id tokens +
  expiry (secrets), issuer/clientId non-secret; `save/load/clear`. Tests:
  round-trip, clear, no secrets in UserDefaults.
- [x] 2.2 Serialized refresh (actor): single-flight; persist rotated refresh
  token BEFORE using the new access token; on failure clear session and mark
  signed-out. Tests: two concurrent refresh calls → one network refresh;
  rotated token persisted; failure → signed-out state.

## 3. Provider configuration + callback scheme

- [x] 3.1 Settings fields: issuer URL + native client id (defaults), "Validate"
  via discovery; validation state/errors surfaced. Tests: URL normalization
  (trailing slash stripped), validation outcomes via stub.
- [x] 3.2 `Info.plist` `CFBundleURLTypes` declaring `com.emergent.memory.connector`;
  verify built app registers the scheme.

## 4. Sign-in / sign-out UX

- [x] 4.1 `AuthStore` (@MainActor ObservableObject): states
  signedOut/signingIn/signedIn/error; `signIn()` via `ASWebAuthenticationSession`
  (parse code+state, verify state, exchange), `signOut()` (revoke + end_session
  + clear + stop engine); expose identity inputs for the account card.
- [x] 4.2 Wire into Project & Account (sign-in button + provider fields) and the
  Connection page (auth-first, manual-token under an advanced/fallback section).
  Manual smoke: sign in, cancel, sign out.

## 5. Project switcher

- [x] 5.1 `MemoryAPIClient`: optional `projectID` → `X-Project-ID` header
  (+ `X-Org-ID` when known); `projects()` (`GET /api/projects`) and `orgs()`
  reuse. Tests: header presence/absence; decode fixtures.
- [x] 5.2 Project store + switcher UI: list user's projects (names; ids behind
  the copy control), select → persist active project (UserDefaults), refresh
  project/org names; empty/loading/error states. Manual smoke: switch projects.
- [x] 5.3 A project switcher is reachable from the sidebar or Project & Account
  page per the spec; window reflects the active project.

## 6. Project connector token + engine wiring

- [x] 6.1 Mint/reuse project token: `POST /api/projects/:projectId/tokens`
  (name `Memory Connector (macOS)`, minimal scopes from the API-token enum
  verified in the clone); store in Keychain under `project-token.<projectId>`;
  reuse when present; tests with stub (create, reuse, failure).
- [x] 6.2 On project selection: ensure token, write engine config
  (`EngineConfigWriter`, token + project_id) and restart the engine; on sign-out
  stop the engine and clear the active project token. Manual smoke: switching
  re-points the relay (hub session moves to the new project).
- [x] 6.3 Token-only fallback preserved (no issuer/clientId → manual token path
  unchanged); tests/regression check on existing settings flow.

## 7. Docs

- [x] 7.1 Operator docs: Zitadel Native app setup (auth method none + PKCE,
  redirect `com.emergent.memory.connector://callback` + post-logout URI,
  Development mode, Token Type JWT), scopes, and troubleshooting (rotation,
  scheme, issuer). Add to connector/README or docs.

## 8. Gate

- [x] 8.1 Hosted tests green (`tools/mac-build.sh --test`), build succeeds.
- [ ] 8.2 Manual macOS smoke: configure provider → sign in → see account →
  switch project → engine registers on the new project → sign out stops the
  engine; record in a session doc.

## 9. Per-project profiles + toolbar switcher (follow-up feedback)

- [x] 9.1 `ProjectProfileStore`: per-project profile (project id → enabled/disabled
  local MCP tools + connector instance id), persisted in UserDefaults keyed by
  project id; load/save/delete; no project active → shared/default values.
  Unit tests (round-trip, isolation between projects, defaults fallback).
- [x] 9.2 Wire profiles through `ProjectStore.selectProject`: build the engine
  config from the project's profile (instance id + disabled tools) instead of
  the global values; editing tools/instance id while a project is active updates
  that project's profile; manual-token (no project) keeps the shared values.
  Unit tests (config written from profile; edits persist per project).
- [x] 9.3 Toolbar project switcher: `.toolbar` menu at the top of the window
  showing the active project (name) and allowing selection; reflects loading/
  signed-out states; switching keeps the engine-status affordance.
- [x] 9.4 Connection/Tools pages reflect the active project's profile (instance
  id + tool toggles edit the profile when a project is active; shared otherwise)
  with clear "applies to <project>" copy.
- [ ] 9.5 Gate: hosted tests + build; install; user verifies switching at the
  top and per-project tool/instance differences.

## 10. Explicit per-project connection (follow-up)

- [ ] 10.1 Per-project profile gains an explicit `connected` flag (default off)
  persisted with the profile; add `ProjectStore.connect(projectID:)` /
  `disconnect(projectID:)` that ensure/reuse the project token, write the engine
  config, and start/stop the engine. Only one project is connected at a time
  (connecting another moves the connection); disconnecting stops the engine.
- [ ] 10.2 Master "all tools" control per project (enable/disable every catalog
  tool with one action) alongside the individual tool toggles; persisted in the
  project profile.
- [ ] 10.3 UI: project management shows each project with a connection toggle
  and an all-tools control (toolbar/split control), plus connection state in the
  footer/dashboard; Tools page keeps per-tool toggles for the active project and
  reflects the master state.
- [ ] 10.4 Gate: hosted tests + build; install; user verifies connect/disconnect,
  all-tools toggle, and that a connected project (even with 0 tools) appears in
  the web UI MCP nodes page.


