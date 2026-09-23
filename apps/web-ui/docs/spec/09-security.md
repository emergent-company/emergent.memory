# 09 — Security

## Trust boundary

The gateway is designed to be served on a **public address** and authenticates
fail-closed by default: `AUTH_MODE=session` (D17) requires a **Zitadel OIDC sign-in** for
the browser UI and **a valid session for `/api/*`**. Session-less `X-API-Key`/device access
was **removed** with the static `MEMORY_TOKEN` (issue #818 tracks a scoped replacement);
`AUTH_MODE=dev` is an **explicit** local-development escape hatch only — it disables browser
auth, carries no Memory credential, and is warned about loudly at startup. The trust boundary
is Zitadel's identity provider, not the network.

```
internet
├─ clients (web/iOS/Mac)  ── session cookie ──► Go app
├─ Go app  ── session token | AGENT_TRIGGER_TOKEN ──► memory
├─ Go app  ── server key  ──► LiveKit (mint JWT)
└─ bridge workers ── per-room scoped token ──► memory (chat)
```

## Authentication layers

1. **Clients → Go app:** in the default session mode (`AUTH_MODE=session`) every `/api/*`
   request must carry a valid session cookie (the browser's access token is the bearer sent
   to memory, with `X-Project-ID`/`X-Org-ID` headers scoping the active project/org). The
   former per-device `X-API-Key` / admin `TOKEN_API_KEY` path is **removed**: it could not
   mint a Memory credential, so it now returns `401 session_required` rather than proxying an
   empty bearer upstream. In the explicit dev mode (`AUTH_MODE=dev`) `/api/*` is gated by
   `requireClientKey` (open when `TOKEN_API_KEY` is unset) and has no Memory credential — it
   is for the local mock backend only.
2. **Go app → memory:** the caller's scoped session token (server-side, never sent to
   clients); the GitHub webhook uses the dedicated `AGENT_TRIGGER_TOKEN`. Project is derived
   from the token.
3. **iOS → LiveKit:** short-lived room-join JWT minted by the Go app. No LiveKit credentials
   in the iOS binary.
4. **Bridge → memory:** short-lived per-room `emt_*` token (chat scope), fetched from the
   internal binding endpoint.

**Web UI:** served same-origin by the Go app. The default `AUTH_MODE=session` requires a
browser sign-in via **Zitadel OIDC**; the explicit `AUTH_MODE=dev` escape hatch has no
browser auth and is for local development only, with no Memory credential. Session sign-in uses **Zitadel OIDC**
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

## GitHub webhook trigger credential (`AGENT_TRIGGER_TOKEN`)

`AGENT_TRIGGER_TOKEN` is a single static `emt_*` bearer the gateway presents to memory
to trigger the PR-review agent. It is a **deliberate, accepted decision** (issue #847),
not an oversight. The rationale, the residual risk, and the future path are recorded here
so the choice stays explicit.

**Why a static shared secret is accepted for this surface:**

- **The webhook ingress is already HMAC-gated.** GitHub authenticates each delivery with an
  `X-Hub-Signature-256` header (`HMAC-SHA256`, verified in constant time via `hmac.Equal` in
  `verifyGitHubSignature`). `AGENT_TRIGGER_TOKEN` is *not* the ingress gate: webhook callers
  never see or present it. It is the gateway→memory credential only, presented as
  `Authorization: Bearer …` on the trigger call.
- **It fails closed on unset/empty.** `Config.Validate` refuses startup when
  `GITHUB_WEBHOOK_SECRET` is set but `AGENT_TRIGGER_TOKEN` is empty, and the handler returns
  `503` (retryable) *before* spawning the async trigger when the token is missing — so a
  missing token can never be accepted and then fail invisibly after the `202`. There is no
  path where an empty token is treated as a valid credential.
- **There is no gateway-side token comparison to be made constant-time** — the token is
  forwarded to memory, which resolves it by SHA-256 hash lookup.

**Accepted residual risk** (why this is not yet a scoped marker credential):

- The token is **shared** (no per-integration identity), **long-lived** (no expiry/forced
  rotation), and carries **no reserved marker**, so no surface restriction can be enforced
  against it server-side. A leaked token grants whatever scopes it was minted with, until an
  operator rotates it.
- Impact is bounded: the token's only use is the PR-review trigger; it is held server-side in
  the gateway's env, never shipped to clients or the web UI, and never logged.

**Rotation:** rotate by minting a fresh `emt_*` token in memory and updating `AGENT_TRIGGER_TOKEN`
in the gateway env (then restart). Rotation is manual; there is no automated expiry.

**Future path:** promote this to a real scoped credential — an `emt_*` token minted via an
internal mint path carrying a reserved marker scope (e.g. `webhook:trigger`, following the
`mcp:agent-call` / `share:agent-chat` precedents in `apps/server/domain/apitoken/`), with an
expiry, a per-integration name, and a scope ceiling enforced on the trigger-agent route. That
is a server-side change and is deliberately out of scope for #847.

## Room allow-list (iOS token)

- `TOKEN_ALLOWED_ROOMS` (comma-separated) if set; else rooms prefixed by a registered agent
  name, falling back to `memory-`.
- Token mint fails closed: LiveKit creds unset → 503; wrong key → 401; disallowed room → 403.

## Threats & mitigations

| Threat | Mitigation |
|---|---|
| Unauthenticated client reads memory data | session auth required by default (`AUTH_MODE=session`); session-less `X-API-Key`/device access removed (401 `session_required`); `dev` is explicit local-dev only, validated + warned at startup, with no Memory credential |
| Client steals LiveKit creds | clients only get short-lived room JWTs |
| Rogue agent escalates via tools | memory tool policies (`ToolPolicies` confirm/disable) + tool allowlists per attachment |
| Stolen one-time setup token | single-use + 10-min TTL; worst case one extra device key, revocable by deleting the setting row |
| Device key leaks | per-device key is scoped to one client and revocable; not memory/LiveKit creds |
| MCP server secrets (headers/env) stored **plaintext** in memory | scope by memory project isolation; do **not** store memory/LiveKit master creds as MCP headers; document the limitation |
| `AGENT_TRIGGER_TOKEN` leaks (static, shared, no marker) | ingress is HMAC-gated (constant time); token held server-side only and never logged; fail-closed on unset; accepted risk with manual rotation, tracked for a reserved-marker scope ceiling (#847) |

## Non-goals (explicitly out)

- Anonymous access. Every UI route requires a signed-in session; `/api/*` requires a session
  or a valid key. TURN/voice relay beyond LiveKit stays out.
- Multi-user roles/permissions. Identity comes from Zitadel sign-in, but tenancy is still
  one memory project per deployment (D6); there is no per-user ACL layer yet.
- Per-client identity beyond the per-device key / Zitadel session (device keys are revocable
  but not a full identity/ACL system).
