## Why

The macOS connector app authenticates with a manually pasted project API token
(`emt_*`): no user sign-in, no identity beyond the token, and one fixed project.
Users want to (a) sign in with the same Zitadel/OIDC identity they use for the
Memory web app, and (b) switch between the Memory projects they belong to,
without re-pasting tokens.

The backend already supports this: the Memory API accepts a **Zitadel user JWT
as `Authorization: Bearer`** (introspection/userinfo fallback); project context
for project-scoped endpoints comes from the `X-Project-ID` header (user JWTs are
not project-bound and bypass `emt_*` scope checks); `GET /api/projects` lists
the user's projects; and `POST /api/projects/:id/tokens` mints a project-scoped
`emt_*` for a member. So the app can log in, list/switch projects, and mint a
stable project token for the headless engine — no backend or gateway changes.

## What Changes

- **Zitadel OIDC sign-in (native app flow)**: Authorization Code + PKCE (S256),
  browser via `ASWebAuthenticationSession`, custom-scheme callback
  (`com.emergent.memory.connector://callback`); issuer + native client id are
  configurable (defaults for the hosted instance). No third-party OAuth SDK in
  v1 (CryptoKit for PKCE).
- **Session handling**: access/refresh/id tokens in the Keychain; access token
  used for user-scoped API calls; **serialized** refresh (Zitadel rotates
  refresh tokens single-use — persist the new one before use); sign-out via
  end_session + revoke + Keychain clear; on refresh failure the app returns to
  the signed-out state instead of silently breaking.
- **Signed-in account UI**: Project & Account shows the authenticated user
  (avatar-initials, name, email) from the user session; the token-only fallback
  keeps working but shows "signed in with an API token" (no user identity).
- **Project switcher**: list the user's projects (`GET /api/projects`), pick
  one (names first, ids behind the copy control), persist the selection, and
  scope Memory calls to it via `X-Project-ID`.
- **Project connector token for the engine**: on project selection, mint (or
  reuse) a project-scoped `emt_*` via `POST /api/projects/:id/tokens` and write
  it to the engine config so the relay keeps working headlessly and survives
  app-session changes; switching projects re-points the engine at that
  project's token (engine restart).
- **Fallback**: manual `emt_*` paste remains for setups without OAuth.
- **Docs**: Zitadel Native-app provisioning steps (auth method `none` + PKCE,
  redirect/post-logout URIs, Development mode for the custom scheme, token type
  JWT) recorded for operators.
- Out of scope: gateway/backend changes, device-authorization flow (possible
  later fallback), role-based UI, multiple simultaneous accounts, image avatars
  (initials only, as today).

## Capabilities

### New Capabilities

- `mac-connector-auth`: Zitadel OIDC sign-in for the Mac app, session/token
  lifecycle (rotating refresh, Keychain), and discovery/selection of Memory
  projects including minting a project-scoped connector token for the engine.

### Modified Capabilities

- `mac-connector-app`: the window shows signed-in account state and a project
  switcher; the connection settings accept either a sign-in or a manual token.

## Impact

- `client/macos/MemoryConnector/`: new auth/OIDC sources (PKCE, token client,
  session store), project list/switch UI, project-token minting, Info.plist
  `CFBundleURLTypes` for the callback scheme; `MemoryAPIClient` gains
  `X-Project-ID` support + project/token endpoints.
- Keychain: new items for OIDC tokens + per-project connector tokens.
- Engine config: uses the minted project token (existing `token` field); no
  engine/connector Go changes required.
- No gateway / emergent.memory changes.
- Testing: deterministic unit tests for PKCE, authorize URL, token exchange
  parsing, serialized refresh, project selection + minting (stubbed HTTP);
  OAuth browser round-trip is a manual macOS smoke.
