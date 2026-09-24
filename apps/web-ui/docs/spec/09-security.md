# 09 — Security

## Trust boundary

The gateway is designed to be served on a **public address** and authenticates
fail-closed by default: `AUTH_MODE=session` (D17) requires a **Zitadel OIDC sign-in** for
the browser UI and **a valid session for `/api/*`**. Session-less access is served by a
**scoped, revocable per-device credential** (see below); the shared `MEMORY_TOKEN` and the
blanket `X-API-Key` path are gone. `AUTH_MODE=dev` is an **explicit** local-development
escape hatch only — it disables browser auth, carries no Memory credential, and is warned
about loudly at startup. The trust boundary is Zitadel's identity provider, not the network.

```
internet
├─ clients (web)  ── session cookie ──► Go app
├─ clients (iOS voice)  ── emt_* device credential ──► Go app (device surface only)
├─ Go app  ── session token | device credential | webhook trigger credential ──► memory
├─ Go app  ── server key  ──► LiveKit (mint JWT)
└─ bridge workers ── per-room scoped token ──► memory (chat)
```

## Authentication layers

1. **Clients → Go app:** in the default session mode (`AUTH_MODE=session`) every `/api/*`
   request must carry a valid session cookie (the browser's access token is the bearer sent
   to memory, with `X-Project-ID`/`X-Org-ID` headers scoping the active project/org). A
   session-less **device client** (the iOS voice app) presents a scoped `emt_*` **device
   credential** (marker scope `device:api`) on `X-API-Key` (its convention) or
   `Authorization: Bearer emt_…` — the same credential, validated identically — which the
   gateway recognises by introspection and accepts on the **device surface only** — room-token
   mint, agent picker, chat/session relay, memory browsing — proxying the credential verbatim
   and deriving project/org from the token, never from raw headers. The blanket **shared**
   `X-API-Key` / `TOKEN_API_KEY` path is **removed**: a bare shared key or a programmatic
   `emt_*` token on the gateway returns `401 session_required` / `401 invalid_device_credential`
   rather than proxying an empty bearer upstream. In the explicit dev mode (`AUTH_MODE=dev`)
   `/api/*` is gated by `requireClientKey` (open when `TOKEN_API_KEY` is unset) and has no
   Memory credential — it is for the local mock backend only.
2. **Go app → memory:** the caller's scoped session token or device credential (server-side,
   never sent to clients); the GitHub webhook uses the scoped `webhook:trigger` credential
   (`AGENT_TRIGGER_TOKEN`). Project is derived from the token.
3. **iOS → LiveKit:** short-lived room-join JWT minted by the Go app. No LiveKit credentials
   in the iOS binary.
4. **Bridge → memory:** the per-room `emt_*` token bound at voice-token mint (for a device
   session, the device credential itself is proxied verbatim), fetched from the internal
   binding endpoint.

**Scoped device credential.** A device credential is an ordinary `core.api_tokens` row with
`user_id = NULL` carrying the reserved `device:api` marker plus the hardcoded read-only scope
set `device:api + agents:read + data:read` (strictly below `project_viewer`). The marker is
reserved to the internal mint path (`CreateDeviceToken`) — no user-facing endpoint can attach
it — and the server enforces an **exact-set ceiling** plus a **surface guard** at validation
time, so even a DB-tampered device token cannot exceed the read-only ceiling or leave the
device surface. Unknown/revoked/expired/store-down all fail closed (401), never proxied.
The credential carries a 90-day default expiry, a `revoked_at` flag, and a best-effort
`last_used_at` touch, so each device is individually revocable and auditable.

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
`/api/*` accepts a valid session **or** a scoped device credential on the device surface.
The access token is refreshed
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
- A **device credential** is minted server-side when the QR is minted (the devices page is
  session-authenticated, so the mint carries the owner's session) and stored alongside the
  one-time token for a single claim; an unclaimed credential is revoked when the QR rotates.
- `POST /api/setup` (no auth) exchanges the one-time token for the device credential
  (`{"serverURL", "tokenEndpoint", "apiBaseURL", "apiKey"}`), where `apiKey` is the `emt_*`
  credential. Unknown/expired/already-used tokens, or a token with no bound credential, → 401.
- The retired 64-hex `ios_device_keys` registry authenticates nothing; device credentials are
  project `core.api_tokens` rows, listed and revoked from the Project Settings page
  (`POST /settings/devices/:id/revoke`), which cuts that client's `/api/*` access immediately.

## Secret handling

- Memory and LiveKit secrets live **only** in the Go app and (for chat) are injected into
  bridge workers via env. Never in client code or the web UI.
- iOS QR onboarding returns the **per-device** device credential + server URL (never
  memory/LiveKit creds); credentials are scoped to one device, read-only, expiring, and
  revocable from Project Settings.
- No secrets in git; compose `.env` is git-ignored.

## GitHub webhook trigger credential (`webhook:trigger`)

The gateway→memory forwarder bearer for the GitHub webhook is a **scoped per-integration
credential**, not a shared static secret. It is an ordinary `core.api_tokens` row carrying
the reserved `webhook:trigger` marker, with a per-integration name, a default expiry, and a
hardcoded scope ceiling enforced at mint time and again at validate time. The gateway still
holds the raw `emt_*` value in `AGENT_TRIGGER_TOKEN` (server-side only, never shipped to
clients or the web UI) and presents it as `Authorization: Bearer …` on the trigger call.

**Operational model (mint → install → use → rotate/revoke).** An operator mints the
credential through the HTTP mint surface — `POST /api/projects/:projectId/webhook-trigger-tokens`
(`Handler.CreateWebhookTriggerToken`, gated by `RequireAuth()` exactly like the device-token
route it mirrors) — which returns the raw `emt_*` value once. The operator installs that
value as the gateway's `AGENT_TRIGGER_TOKEN`. The credential validates as marker-class and
ceiling-bound, expires on its default 90-day schedule, and is rotated via the shared
project-token `Regenerate` surface or revoked via `Revoke`; `last_used_at` attributes use.
The gateway holds **one** forwarder credential today — simultaneous per-integration
forwarding (one gateway-held credential per integration, selected per delivery) is not
implemented.

**The ingress gate is unchanged — HMAC.** GitHub authenticates each delivery with an
`X-Hub-Signature-256` header (`HMAC-SHA256`, verified in constant time via `hmac.Equal` in
`verifyGitHubSignature`). The webhook credential is *not* the ingress gate: webhook callers
never see or present it. It is the gateway→memory credential only, resolved server-side by
SHA-256 hash lookup — there is no gateway-side token comparison to make constant-time.

**It still fails closed on unset/empty.** `Config.Validate` refuses startup when
`GITHUB_WEBHOOK_SECRET` is set but `AGENT_TRIGGER_TOKEN` is empty, and the handler returns
`503` (retryable) *before* spawning the async trigger when the token is missing — so a
missing token can never be accepted and then fail invisibly after the `202`.

**Scoped credential bounds (what #859 added over #847's accepted residual risk):**

- **Reserved marker + mint surface.** The credential carries `webhook:trigger`, reserved to
  the internal mint path — no user-facing token endpoint can attach it (`Create`,
  `CreateAccountToken`, `UpdateScopes`, `UpdateAccountTokenScopes` all reject it) — and is
  minted via `POST /api/projects/:projectId/webhook-trigger-tokens` (mirroring the
  device-token route's `RequireAuth()` gate).
- **Hardcoded ceiling.** The stored scope set is exactly `webhook:trigger + agents:read +
  agents:write + data:read` (the `agents:write` the trigger route's own gate requires, plus
  the read family the review agent's in-process loopback uses). It is enforced at mint time
  and again at validate time: a DB-tampered row whose scope set exceeds the ceiling is
  rejected fail-closed, never trusted.
- **Surface guard.** The server rejects the credential on any path outside the webhook
  trigger surface (the trigger route plus its `search-knowledge`→`/query` loopback). Even a
  leaked credential cannot reach token-mint, member, org, chat, skills or write endpoints.
- **Per-integration identity, expiry, revocation, attribution.** Each integration gets its
  own named `core.api_tokens` row (its own `last_used_at`), a default expiry, and an
  individual `revoked_at`; rotation reuses the shared project-token `Regenerate` path.
  Unknown/revoked/expired/store-down all deny (401), never proxied.

**Effective-scope note.** `agents:write` is an umbrella scope that expands to `chat:admin`
and `skills:write`, and `agents:read`/`data:read` expand to the read family. The
credential's **effective** scope set therefore includes those implied scopes; they are
reachable only through the review agent's own loopback (a trusted, bounded use), because the
surface guard confines the credential to the trigger route and its query loopback.

**Rotation:** rotate by minting a fresh webhook credential (mint surface), updating
`AGENT_TRIGGER_TOKEN` in the gateway env, then restarting; or use the shared project-token
`Regenerate`/`Revoke` surface. The credential carries a default 90-day expiry so it does not
live forever.

## Room allow-list (iOS token)

- `TOKEN_ALLOWED_ROOMS` (comma-separated) if set; else rooms prefixed by a registered agent
  name, falling back to `memory-`.
- Token mint fails closed: LiveKit creds unset → 503; wrong key → 401; disallowed room → 403.

## Threats & mitigations

| Threat | Mitigation |
|---|---|
| Unauthenticated client reads memory data | session auth required by default (`AUTH_MODE=session`); device access is via a read-only scoped credential, fail-closed on the device surface; `dev` is explicit local-dev only, validated + warned at startup, with no Memory credential |
| Client steals LiveKit creds | clients only get short-lived room JWTs |
| Rogue agent escalates via tools | memory tool policies (`ToolPolicies` confirm/disable) + tool allowlists per attachment |
| Stolen one-time setup token | single-use + 10-min TTL; worst case one extra device credential, revoked on QR rotation and revocable from Project Settings |
| Device credential leaks | per-device credential is scoped to one client, read-only, expiring, and revocable; the exact-set ceiling + surface guard bound it even if tampered |
| MCP server secrets (headers/env) stored **plaintext** in memory | scope by memory project isolation; do **not** store memory/LiveKit master creds as MCP headers; document the limitation |
| `AGENT_TRIGGER_TOKEN` leaks (webhook trigger credential) | ingress is HMAC-gated (constant time); credential is scoped (`webhook:trigger` marker, exact-set ceiling, surface guard), per-integration, expiring, revocable; held server-side only and never logged; fail-closed on unset (#859) |

## Non-goals (explicitly out)

- Anonymous access. Every UI route requires a signed-in session; `/api/*` requires a session
  or a scoped device credential on the device surface. TURN/voice relay beyond LiveKit stays out.
- Multi-user roles/permissions. Identity comes from Zitadel sign-in, but tenancy is still
  one memory project per deployment (D6); there is no per-user ACL layer yet.
- Per-client identity beyond the per-device credential / Zitadel session (device credentials
  are revocable and auditable but not a full identity/ACL system).
