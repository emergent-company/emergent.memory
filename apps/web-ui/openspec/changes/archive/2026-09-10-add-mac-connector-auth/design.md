# Design — add-mac-connector-auth

## Context

Ground truth from recon (see exploration results):
- Memory API accepts a **Zitadel user JWT as Bearer** (introspection → userinfo →
  local JWT fallback, `pkg/auth/middleware.go:502-568`); project context for
  project-scoped endpoints comes from the **`X-Project-ID`** header
  (`middleware.go:134-196`); user JWTs bypass `emt_*` scope checks
  (`middleware.go:368-385`).
- `GET /api/projects` lists the user's projects (`[]ProjectDTO{id,name,orgId}`);
  `GET /api/orgs` gives org names.
- `POST /api/projects/:projectId/tokens` mints a project-scoped `emt_*`
  (membership-checked) and returns the token once.
- mcprelay `GET /api/mcp-relay/connect` requires only `RequireAuth`; project =
  emt_ token's project **or** `X-Project-ID` (doc mentions a `projectId` query
  param but code ignores it).
- Gateway OIDC = authorization code + PKCE(S256), scopes
  `openid profile email offline_access`, token/refresh calls without a client
  secret; no native/device flow there. Zitadel supports Native apps with PKCE.
- Zitadel: access token is **opaque by default** (app Token Settings must select
  JWT for local validation); **refresh tokens are single-use and rotate**; custom
  schemes need Development mode; issuer has no trailing slash.

## Goals / Non-Goals

Goals: sign-in via the instance's Zitadel provider; a valid user session for
identity + project listing; a project switcher; a stable project token for the
headless engine; token-only fallback intact.

Non-goals: backend/gateway changes, device-code flow, multi-account, role UI,
image avatars, changing the connector Go module.

## Decisions

### 1. Native OAuth via ASWebAuthenticationSession + custom scheme, no SDK

`ASWebAuthenticationSession(url:callbackURLScheme:)` with scheme
`com.emergent.memory.connector` (`com.emergent.memory.connector://callback`),
declared in `CFBundleURLTypes`. PKCE implemented with CryptoKit
(`SHA256(verifier)` → base64url), random `state`/`nonce`. No AppAuth dependency
in v1 (fewer moving parts; the app is not sandboxed). Loopback-listener flow is
a documented alternative if a custom scheme proves awkward for an instance.

### 2. Provider config with discovery validation

Settings gain issuer URL + native client id (defaults for the hosted instance).
"Validate" hits `{issuer}/.well-known/openid-configuration` and checks the
authorization/token endpoints. Zitadel app setup (operator prerequisite) is
documented in `connector/README` / design: application type **Native**, auth
method **none (PKCE)**, redirect `com.emergent.memory.connector://callback` +
matching post-logout URI, **Development mode** for the non-HTTPS scheme, **Token
Type: JWT** (recommended), refresh tokens enabled.

### 3. Two token planes: user session for REST, minted project token for the engine

- **User session** (access/refresh/id) drives identity, project listing, and
  token minting. It is short-lived and rotates; it must not be what the headless
  engine depends on.
- **Project connector token** (`emt_*`, minted per project) is written to the
  engine config and drives the relay. It is long-lived, project-bound, and
  survives app-session changes — the engine keeps relaying even if the user
  session expires, and the browser is never needed for the relay.

Rationale: refresh-rotation failure (offline/revoked) would otherwise kill the
connector; the API-token path is also exactly what the engine already supports.
Minting scope: pick the minimal `emt_*` scopes that cover relay + status from
the API-token scope enum (verified against the clone at implementation), named
`Memory Connector (macOS)`; reuse an existing stored token per project.

### 4. Keychain schema + serialized refreshing

Service `com.emergent.memory.connector`, accounts: `oidc.access`,
`oidc.accessExpiry`, `oidc.refresh`, `oidc.id`, `oidc.issuer`, `oidc.clientId`
(issuer/clientId may live in UserDefaults as non-secret; tokens only in
Keychain), and `project-token.<projectId>`. Active project id in UserDefaults.

Refresh is an **actor** (serial single-flight): if two callers need a refresh,
one performs it; the rotated refresh token is **persisted before** the new
access token is used; any failure clears the session and flips the app to
signed-out with a re-login prompt (no silent breakage).

### 5. Project context on API calls

`MemoryAPIClient` gains an optional `projectID`; when set it sends
`X-Project-ID: <uuid>` (and can send `X-Org-ID` when known). Identity endpoints
(`auth/me`, `user/profile`) don't need it; project-scoped calls do. The switcher
persists the active project and re-points subsequent calls.

### 6. Project switching re-points the engine

On selection: ensure a connector token exists for the project (Keychain, else
mint), write `~/.config/memory-connector.yml` with that token (existing
`EngineConfigWriter`; include `project_id` for clarity), and restart the engine.
Switching is therefore a deliberate engine restart; the UI shows the active
project and engine state.

### 7. Sign-out

`POST {issuer}/oauth/v2/revoke` for the refresh token, then `end_session` with
`id_token_hint` + registered post-logout redirect (browser), clear all Keychain
items for the session and project tokens, stop the engine. Token-only setups are
unaffected.

### 8. Fallback unchanged

If no issuer/client id is configured, the app behaves as today: manual `emt_*`
paste drives the engine, identity shows "unavailable / API token", and the
sign-in affordance is present but inert until configured.

## Risks / Trade-offs

- [Rotating single-use refresh loss] → actor-serialized refresh + persist-before-
  use + fail-to-signed-out (no half-broken session).
- [Opaque access token] → we do not validate JWTs locally; we pass the token to
  Memory, which introspects. Document the recommended JWT token-type setting.
- [Custom scheme rejected outside Development mode] → documented provisioning
  step; loopback alternative noted.
- [Minting scope mismatch] → scopes chosen from the API-token enum at
  implementation; relay needs none, status reads sessions; verified against the
  clone. If a min-scope token is rejected anywhere, fall back to the documented
  read/relay scope set.
- [Per-project token sprawl] → reuse stored tokens keyed by project; a project
  token is only minted once per project (or when the user explicitly regenerates).
- [Engine restart churn on switch] → acceptable (explicit user action); keep the
  restart fast and surface state in the UI.

## Migration Plan

Additive. Existing manual tokens keep working; first sign-in migrates the app to
OAuth and mints the project token. Rollback = clear OIDC settings and paste a
token again. No backend changes.

## Open Questions

- Exact `emt_*` scope keys for relay/status (confirm from `apitoken` enum).
- Whether the deployed Zitadel app can use a public/native client without a
  secret (gateway uses none — assume yes) and whether it exposes a
  `device_authorization_endpoint` (only relevant if we add device flow later).
- Whether the gateway should later proxy a "native client config" endpoint so
  the app doesn't need the issuer/client id typed in; not required for v1.
