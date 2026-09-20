## Why

An owner wants to let outsiders — prospects, users, teammates without an account — talk to a single configured agent without provisioning them an org membership or a full project account. Today every agent surface is authenticated and project-scoped: there is no public, keyed path to one agent. This change adds a public, keyed, shareable agent page: an owner mints a link containing a key, and anonymous end users (no login) chat with that one agent and see a list of their own active sessions.

The server deliberately keeps **zero unauthenticated routes**. Every share route is `RequireAuth` + the `share:agent-chat` marker scope + token→link binding resolution. The web gateway is the BFF for the public surface: it hosts the public page, exchanges the URL-fragment key once for a sealed HttpOnly cookie, and forwards the key as a Bearer credential plus a cryptographically signed end-user reference to the authenticated server endpoints. Project/org are always resolved from the bound share link, never from client-supplied headers.

## What Changes

- **Credential reuses `core.api_tokens` with a reserved marker scope, maximally scoped.** A new marker scope `share:agent-chat` (alongside `mcp:agent-call`) is minted only by the internal `CreateAgentChatShareToken` path, plus a binding table `kb.agent_share_links(api_token_id UNIQUE)`. The minted key carries **only** `share:agent-chat` (no `projects:read`) and `user_id = NULL` (project_id still set); user-facing `Create`/`UpdateScopes`/account-token paths reject the reserved scope. A guard in `RequireAuth` (`rejectShareTokenOutsideSurface`) returns 403 for any request carrying the marker outside `/api/share/agent`, so a leaked share key is unusable on project/member endpoints (mirroring the `mcp:agent-call` precedent).
- **Authorization requires BOTH the marker scope AND token→link binding resolution.** Scope membership alone is insufficient; `RequireShareLink` resolves token → link → agent definition → project/org. Missing → 401, revoked/expired → 410.
- **Revocation cascades to the credential.** `RevokeLink`/`DeleteLink` revoke the bound `core.api_tokens` row immediately (not just the link row), so the share key stops working the moment the link is revoked, regardless of the link's own `expires_at`.
- **End-user identity is cryptographically authenticated.** The gateway mints a UUID v4 `end_user_ref`, seals it (with the share key) into an AES-256-GCM HttpOnly cookie named `memory_share`, and signs it per-request as `X-End-User-Ref` + `X-End-User-Ref-Sig` = base64url-nopad HMAC-SHA256(`SHARE_REF_SECRET`, end_user_ref). The server verifies constant-time: missing/invalid → 401, unconfigured secret → 503 fail-closed, and the ref must exist in `kb.agent_share_end_users`. Body/query `endUserRef` is never accepted — it was removed from the server wire DTOs.
- **Project/org come only from the bound link.** Client `X-Project-ID`/`X-Org-ID` headers are ignored by the server and never sent by the gateway's share client.
- **Session ownership uses a join table** `kb.agent_share_sessions` (NOT nullable columns on `kb.acp_sessions`); visibility is per end user.
- **Share sessions are hidden from normal listings.** Share-created `kb.acp_sessions` rows are excluded from the standard project ACP/chat session listings (`ListACPSessions` adds `NOT EXISTS` against `kb.agent_share_sessions`), so anonymous public-share sessions never appear in a project member's session list.
- **Email capture** (default required) is stored in `kb.agent_share_end_users`; email is metadata only and NEVER grants session access (access keys off `end_user_ref` only).
- **Sandbox is config-gated, default OFF.** When OFF the run's `SandboxConfig` is nil; the executor never mints an ephemeral org token for share runs (`DisableAuthMint`) and never injects the owner's project credentials (`AuthToken` empty).
- **Anonymous tool approvals are supported:** the share-scoped respond endpoint verifies question→run→`acp_session_id`→`kb.agent_share_sessions` ownership for the caller's `end_user_ref`, then drives the shared `RespondToQuestion` resume helper. A per-link deny set (`ShareToolDeny`) is enforced in the executor **before** the confirm gate, so an approval can never override a deny-listed tool. Approval audit rows carry `kb.agent_tool_approvals.share_link_id`.
- **Budget is reserved atomically.** `ReserveShareBudget` locks the `kb.agent_share_usage` `(link_id, period_start)` row with `SELECT … FOR UPDATE` and increments the message count inside the transaction, so concurrent turns for one link cannot all pass a check-then-act gate. Message slots are reserved up-front; tokens/cost are settled after the run against the same rolling-window bucket.
- **Rate limiting at the gateway edge:** per direct socket peer (traefik) always, and per share link (SHA-256 of the token) when a token is present, in-memory and therefore per-instance. The per-IP dimension is advisory (non-spoofable but not per-end-user); the per-link bucket is the authoritative dimension.
- **Hashed-IP audit:** `kb.agent_share_access_log` records only a SHA-256 hex digest of the client IP (never a raw IP).
- **Gateway config rehydration:** `GET /share/api/config` is a cookie-gated route that returns the sanitized public config for the bound link (never the key/token, never project/org identifiers) so a plain refresh or return visit without a URL fragment can rehydrate without re-running the key exchange.
- **Transcript is surfaced end-to-end:** the gateway now carries the session detail (`messages`) through so resuming a session renders its history; the server's `GET /api/share/agent/sessions/:id` transcript shape is `{id, title, isArchived, messages: [{role, content}], createdAt}`.
- **Sentry is intentionally absent from the public share page:** the strict share CSP disallows third-party scripts and outbound reporting, which blocks the Sentry CDN. This is a deliberate consequence of the public-surface CSP, not an omission.

### Defaults

| Setting | Default |
|---|---|
| Link expiry | 30 days |
| Budget | 500 messages / 200k tokens / $5 per rolling 24h |
| Max active sessions per end user | 5 |
| Max concurrent runs per link | 3 |
| Max approvals per session | 10 |
| Approval (suspended-run) auto-cancel | 10 min |
| Max message length | 8,000 chars |
| Rate limit | 30 req/min per socket peer (advisory, burst 10); 60 msg/min per link (authoritative, burst 20) |
| Retention after last activity | 90 days |
| `require_email` | true |
| `show_session_list` | true |
| `sandbox_enabled` | false |
| `allow_end_user_approvals` | true |

### Non-goals

File upload, a public approval UI beyond approve/deny, cross-session memory, custom domains, white-label theming, and an owner analytics dashboard are explicitly out of scope. Email **verification** and retention **jobs** are deferred to Phase 2 and not shipped here.

## Capabilities

### New Capabilities

- `agent-public-share`: server-side share-link lifecycle — the reserved-scope credential and its binding, authorization that requires both scope and binding (plus the out-of-surface 403 guard), owner management API (create/list/get/update/revoke/rotate/reveal/usage), per-link budget/expiry, and the per-link tool deny/allowlist.
- `agent-public-share-runtime`: server-side anonymous end-user runtime — the signed `X-End-User-Ref` identity, the `kb.agent_share_sessions` join-table session ownership (hidden from normal listings), metadata-only email capture, anonymous tool approvals, the sandbox gate, the suspended-run TTL reaper, and the transcript endpoint.
- `web-agent-public-share`: the gateway public surface — the `/share/` page, the sealed-cookie key exchange and per-request HMAC ref signing, the cookie-gated config rehydration route, header strip and log redaction, edge rate limiting, hashed-IP audit passthrough, noindex/CSP/referrer hardening, and the session-list/transcript and approve/deny surface.

### Modified Capabilities

None. Existing semantics — the reserved-scope mint pattern and per-key binding in `agent-mcp-keys`/`agent-mcp-shares`, the run lifecycle and resume contract in `acp-run-lifecycle`, session containment in `acp-sessions`, and tool approval policy in `tool-approval-policy` — are referenced and reused, not restated or redefined. The share-specific approvals and reaper requirements live in the new capabilities above.

## Impact

- `apps/server/domain/apitoken/`: reserved `share:agent-chat` marker scope in `ValidApiTokenScopes`; `CreateAgentChatShareToken` internal mint path (the only caller allowed to set it; mints `user_id = NULL`, scope `share:agent-chat` only); `rejectReservedScopes` gates user-facing create/update paths; `Repository.Create` now returns the id.
- `apps/server/pkg/auth/middleware.go`: `rejectShareTokenOutsideSurface` guard returns 403 for the marker outside `/api/share/agent` (with `share_scope_test.go` unit tests).
- `apps/server/domain/agents/`: new `share_entity.go`, `share_handler.go`, `share_service.go`, `share_store.go`, `share_reaper.go`, `share_test.go`; `entity.go` adds `AgentToolApproval.ShareLinkID`; `executor.go` adds `ExecuteRequest.ShareToolDeny`/`ShareLinkID`/`DisableAuthMint` and enforces the deny set before the confirm gate; `handler.go` extracts the shared `RespondToQuestion`/`RespondParams` resume helper; `repository.go` excludes share sessions from `ListACPSessions` and adds `ReserveShareBudget` (FOR UPDATE); `module.go` wires the share service, handler, and reaper.
- `apps/server/internal/config/config.go`: `SHARE_REF_SECRET` (unset = identity verification fails closed).
- `apps/server/migrations/00160_agent_share_links.sql`: `kb.agent_share_links`, `kb.agent_share_sessions`, `kb.agent_share_end_users`, `kb.agent_share_usage`, `kb.agent_tool_approvals.share_link_id`, `kb.agent_share_access_log`.
- `apps/web-ui/gateway/`: `share.go` (sealed-cookie exchange, `GET /share/api/config` rehydration, session detail/transcript, SSE proxy), `share_middleware.go` (socket-peer per-IP + per-link rate limiting, security headers), `share_owner.go`, `memory_share.go`, `request_logger.go`, `share_page.templ`, `share_owner.templ`, `share_manage.templ`, `webui/static/js/share-agent.js`; `config.go` adds the share env vars; `main.go` registers the `/share/*` group and owner routes and constructs the rate limiters; `auth.go` allowlists `/share/` in the public auth path; `handlers.go` holds the limiter fields.
- `apps/web-ui/tests/e2e/`: new `share.config.ts` + `specs/share/` Playwright project; `mock-memory.mjs`, `playwright.config.ts`, `run-e2e.sh` extended for the share surface.
- No change to the server's route-auth posture: the server continues to expose only authenticated endpoints; the gateway forwards the share credential as Bearer plus the signed ref headers.

## Phases

- **Phase 1a** — credential + binding resolver (incl. out-of-surface 403 guard + revocation cascade); gateway public gate + sealed-cookie exchange + config rehydration + header strip + log redaction; share session join table + signed ref identity + per-ref isolation + email capture + hidden-from-listings; owner management API; atomic budget/rate; sandbox default-off; tool deny/allowlist; noindex/CSP/referrer; migration. ✅ shipped
- **Phase 1b** — public approval path (respond endpoint, question delivery, TTL reaper, hard-deny enforcement, audit). ✅ shipped
- **Phase 2** — owner management UI wiring and rotation/expiry UI (✅ shipped); email verification, usage analytics UI, retention jobs (⏳ deferred).
- **Phase 3** — embed/iframe, branding, cross-device restore (⏳ deferred).
