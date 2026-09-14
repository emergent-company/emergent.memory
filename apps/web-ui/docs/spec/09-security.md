# 09 — Security

## Trust boundary

The gateway is designed to be served on a **public address** and authenticates
fail-closed by default: `AUTH_MODE=session` (D17) requires a **Zitadel OIDC sign-in** for
the browser UI and a valid session or `X-API-Key` for `/api/*`. `AUTH_MODE=dev` is an
**explicit** local-development escape hatch only — it disables browser auth and is warned
about loudly at startup. The trust boundary is Zitadel's identity provider, not the
network.

```
internet
├─ clients (web/iOS/Mac)  ── session cookie | X-API-Key ──► Go app
├─ Go app  ── emt_* token ──► memory
├─ Go app  ── server key  ──► LiveKit (mint JWT)
└─ bridge workers ── emt_* token ──► memory (chat)
```

## Authentication layers

1. **Clients → Go app:** per-device `X-API-Key` (issued by the QR setup flow, stored in
   memory under `ios_device_keys`), or the optional admin key `TOKEN_API_KEY`. In the default
   session mode (`AUTH_MODE=session`) a request carries **either** a valid session cookie
   **or** a valid `X-API-Key`; the browser uses the session (its access token is the bearer
   sent to memory, with `X-Project-ID`/`X-Org-ID` headers scoping the active project/org).
   In the explicit dev mode (`AUTH_MODE=dev`) every `/api/*` request must carry a valid key
   (and is open when `TOKEN_API_KEY` is unset).
2. **Go app → memory:** scoped `emt_*` token (server-side only, never sent to clients).
   Scopes: `agents:read agents:write chat:use data:read data:write schema:read schema:write
   projects:read projects:write admin`. Project is derived from the token.
3. **iOS → LiveKit:** short-lived room-join JWT minted by the Go app. No LiveKit credentials
   in the iOS binary.
4. **Bridge → memory:** `emt_*` token (chat scope), injected by the supervisor.

**Web UI:** served same-origin by the Go app. The default `AUTH_MODE=session` requires a
browser sign-in via **Zitadel OIDC**; the explicit `AUTH_MODE=dev` escape hatch has no
browser auth and is for local development only (`TOKEN_API_KEY` is an optional admin key
gating non-browser clients). Session sign-in uses **Zitadel OIDC**
(authorization-code + PKCE): the gateway issues a **stateless HMAC-signed session cookie**
carrying `access_token`, `refresh_token`, `active_project_id`, `org_id`, and identity
`name`/`email`/`picture` (decoded from the id_token after the gateway verifies the
 token — signature against the provider JWKS plus `iss`/`aud`/`exp`, with the `nonce`
 still bound to the authorization request). Zitadel may not emit a `name`/`email` claim; when
 either is absent the gateway lazily backfills it from the signed-in Memory profile
 (`GET /api/user/profile`, whose `email` field the gateway maps) for the account menu and the
 `/profile` identity card. UI routes require a session (302 → `/auth/login`);
`/api/*` accepts a valid session **or** a valid `X-API-Key`. The access token is refreshed
server-side via `grant_type=refresh_token` (5-min grace window; expired-but-authentic cookies
recover from the refresh token) so sessions don't hard-expire. The session cookie's browser
lifetime is decoupled from the short-lived access token: `SESSION_MAX_AGE` (default 30 days)
keeps the cookie — and its refresh token — retained across access-token expiry; the signed
claims still enforce access-token expiry server-side and the refresh flow silently renews.
The effective "stay signed in" ceiling is the Zitadel refresh-token lifetime, not the access
token. Logout clears the local cookie first (local sign-out never depends on Zitadel), then
redirects to the discovered Zitadel `end_session` endpoint with `id_token_hint`/`client_id`
(`post_logout_redirect_uri` only when `PUBLIC_BASE_URL` is set); if discovery has no
`end_session_endpoint` it falls back to `/` (Sign out in the sidebar footer).

## Device setup (QR onboarding)

- The Project Settings page renders a QR encoding `{"setupURL", "token"}`; the current
  token is persisted in memory's settings (survives gateway restarts), single-use, and
  expires 10 minutes after minting (reused across page reloads while valid; a fresh one
  is minted once it is consumed or expired).
- `POST /api/setup` (no auth) exchanges the one-time token for a per-device key
  (`{"serverURL", "tokenEndpoint", "apiBaseURL", "apiKey"}`). Unknown/expired/already-used
  tokens → 401.
- Per-device keys are random 32-byte hex strings stored in a single registry
  setting (`ios_device_keys` / `registry` / `{"devices": {...}}`); revoke one
  from the Project Settings page (`POST /settings/devices/:key/revoke`), which
  cuts that client's `/api/*` access immediately.

## Secret handling

- Memory and LiveKit secrets live **only** in the Go app and (for chat) are injected into
  bridge workers via env. Never in client code or the web UI.
- iOS QR onboarding returns the **per-device** API key + server URL (never memory/LiveKit
  creds); keys are scoped to one device and revocable from Project Settings.
- No secrets in git; compose `.env` is git-ignored.

## Room allow-list (iOS token)

- `TOKEN_ALLOWED_ROOMS` (comma-separated) if set; else rooms prefixed by a registered agent
  name, falling back to `memory-`.
- Token mint fails closed: LiveKit creds unset → 503; wrong key → 401; disallowed room → 403.

## Threats & mitigations

| Threat | Mitigation |
|---|---|
| Unauthenticated client reads memory data | session auth required by default (`AUTH_MODE=session`); `dev` is explicit local-dev only, validated + warned at startup |
| Client steals LiveKit creds | clients only get short-lived room JWTs |
| Rogue agent escalates via tools | memory tool policies (`ToolPolicies` confirm/disable) + tool allowlists per attachment |
| Stolen one-time setup token | single-use + 10-min TTL; worst case one extra device key, revocable by deleting the setting row |
| Device key leaks | per-device key is scoped to one client and revocable; not memory/LiveKit creds |
| MCP server secrets (headers/env) stored **plaintext** in memory | scope by memory project isolation; do **not** store memory/LiveKit master creds as MCP headers; document the limitation |

## Non-goals (explicitly out)

- Anonymous access. Every UI route requires a signed-in session; `/api/*` requires a session
  or a valid key. TURN/voice relay beyond LiveKit stays out.
- Multi-user roles/permissions. Identity comes from Zitadel sign-in, but tenancy is still
  one memory project per deployment (D6); there is no per-user ACL layer yet.
- Per-client identity beyond the per-device key / Zitadel session (device keys are revocable
  but not a full identity/ACL system).
