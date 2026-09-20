## 1. Phase 1a — credential + binding resolver

- [x] 1.1 Add reserved marker scope `share:agent-chat` to the apitoken reserved set (alongside `mcp:agent-call`); reject it in user-facing create/update paths with a "reserved" error
- [x] 1.2 Add internal mint path `CreateAgentChatShareToken` (clone of the `mcp:agent-call` mint pattern) carrying **only** `share:agent-chat` (no `projects:read`), `user_id = NULL`
- [x] 1.3 Add `kb.agent_share_links` model + store: `api_token_id` UNIQUE binding to one agent definition, project/org id, label, config, expiry, revocation
- [x] 1.4 Add share authorization resolver that requires BOTH the `share:agent-chat` marker scope AND token→link binding resolution (scope alone denies; revoked/expired → 410)
- [x] 1.5 Resolve project/org exclusively from the bound share link; ignore/strip any client `X-Project-ID`/`X-Org-ID`
- [x] 1.6 Add `rejectShareTokenOutsideSurface` guard in `RequireAuth`: 403 for any request carrying `share:agent-chat` outside `/api/share/agent`
- [x] 1.7 Unit tests: reserved-scope rejection, mint-path-only marker, binding uniqueness, scope-without-binding denied, binding-without-scope denied, revoked/expired 410, project/org resolution ignores client headers, out-of-surface 403 (`share_scope_test.go`) (TDD)
- [x] 1.8 Migration `00160_agent_share_links.sql` (creates `kb.agent_share_links`, `kb.agent_share_sessions`, `kb.agent_share_end_users`, `kb.agent_share_usage`, `kb.agent_tool_approvals.share_link_id`, `kb.agent_share_access_log`) + `apps/server/internal/testutil/schema.sql` snapshot update

## 2. Phase 1a — gateway public gate + sealed-cookie exchange

- [x] 2.1 Allowlist `/share/` in the gateway public auth path (server routes stay authenticated; gateway forwards the key as Bearer)
- [x] 2.2 Extract the key from the URL fragment; exchange once for an AES-256-GCM sealed HttpOnly `memory_share` cookie carrying the key + end-user ref
- [x] 2.3 Sign the end-user ref per-request as `X-End-User-Ref` + `X-End-User-Ref-Sig` (HMAC-SHA256 over `SHARE_REF_SECRET`); fail closed when the secret is unset
- [x] 2.4 Send no client `X-Project-ID`/`X-Org-ID` on share requests; redact `key`/`token` query params from access logs
- [x] 2.5 Set noindex, `Referrer-Policy: no-referrer`, `X-Content-Type-Options: nosniff`, and a restrictive CSP (incl. `frame-ancestors 'none'` + `'unsafe-inline'`) scoped to `/share/*`
- [x] 2.6 Add cookie-gated `GET /share/api/config` rehydration (sanitized config only, never the key)
- [x] 2.7 Unit tests: public gate allows only `/share/`, fragment→cookie single exchange, unsigned-ref fail-closed, header strip, log redaction, config rehydration returns sanitized config (TDD)

## 3. Phase 1a — share session join table + end-user identity + email

- [x] 3.1 Add `kb.agent_share_sessions` join table linking (share link, `kb.acp_sessions`, end-user ref) — NOT nullable columns on `kb.acp_sessions`
- [x] 3.2 Add `kb.agent_share_end_users` for identity + email capture (access keys off `end_user_ref`; email is metadata only and never grants access)
- [x] 3.3 Enforce per-end-user visibility of sessions (each end user sees only their own) with `filter=active|all|archived` (default active)
- [x] 3.4 Enforce max 5 active sessions per end user, max 3 concurrent runs per link, and max 8,000 chars per message
- [x] 3.5 Transcript endpoint `GET /api/share/agent/sessions/:id` → `{id,title,isArchived,messages:[{role,content}],createdAt}` scoped to the verified ref; gateway surfaces the detail end-to-end
- [x] 3.6 Exclude share-created `kb.acp_sessions` from the standard project ACP/chat session listings (`ListACPSessions` NOT EXISTS)
- [x] 3.7 Unit tests: join-table ownership, per-ref isolation, filter behavior, session cap, concurrent-run cap, message cap, email-never-grants-access, transcript scoping, listings exclusion (TDD)

## 4. Phase 1a — owner management API

- [x] 4.1 Endpoints to create/list/get/update/revoke/rotate a share link; create and rotate return the key exactly once
- [x] 4.2 On-demand reveal endpoint `GET /api/projects/:projectId/share-links/:linkId/reveal` → `{key}`; non-recoverable → 422 `not_recoverable` (list rendering never returns the key)
- [x] 4.3 Revocation cascades: `RevokeLink`/`DeleteLink` revoke the bound `core.api_tokens` row immediately, not just the link row
- [x] 4.4 Apply link config + defaults table (expiry 30d, budget 500 msgs / 200k tokens / $5 per rolling 24h, retention 90d, `require_email` true, `show_session_list` true, `sandbox_enabled` false, `allow_end_user_approvals` true)
- [x] 4.5 Per-link tool deny set (`defaultDangerousShareTools`) hard-blocked unless re-enabled via `tool_allowlist`
- [x] 4.6 Unit tests: create-once key return, reveal + not_recoverable, rotate swaps credential and keeps link, revoke immediately denies + revokes token, defaults applied, deny/allowlist enforced (TDD)

## 5. Phase 1a — budget / rate / sandbox

- [x] 5.1 Per-link rolling 24h budget enforced server-side: `ReserveShareBudget` locks the `kb.agent_share_usage` `(link, period)` row `FOR UPDATE` and reserves the message slot atomically; tokens/cost settled after the run
- [x] 5.2 Gateway edge rate limiting: per direct socket peer (advisory, default 30/min burst 10) and per link — SHA-256 of the token (authoritative, default 60/min burst 20); in-memory, per-instance, bounded keys; exchange is per-IP only
- [x] 5.3 Sandbox config-gated, default OFF (nil `SandboxConfig`); executor never mints an ephemeral org token (`DisableAuthMint`) and never injects owner credentials
- [x] 5.4 Unit tests: atomic reservation serializes concurrent turns, budget exhaustion denies, rolling window resets, rate limiter per-socket-peer/per-link + exchange-only-per-IP, sandbox-off default, no-owner-credential minting (TDD)

## 6. Phase 1b — public approval path

- [x] 6.1 Share-scoped respond endpoint bound to (link, `end_user_ref`) via question→run→`acp_session_id`→`kb.agent_share_sessions` ownership chain
- [x] 6.2 Deliver pending questions (including `ask_user` and tool approvals) to the public session
- [x] 6.3 Enforce the deny set BEFORE the confirm gate in the executor, so approval can never override a deny-listed tool (hard-deny enforcement)
- [x] 6.4 Record `kb.agent_tool_approvals.share_link_id` audit correlation on share-run approvals
- [x] 6.5 Enforce max 10 approvals per session; TTL reaper auto-cancels stale `input-required` runs after the approval timeout (default 10 min)
- [x] 6.6 Unit tests: anonymous respond resumes run, pending-question delivery, deny-before-confirm ordering, ownership verification, approval cap, reaper cancels stale input-required, audit correlation (TDD)

## 7. Phase 2 — owner management UI + verification + analytics + retention

- [x] 7.1 Owner management UI wiring for create/list/revoke/rotate share links (`/agents/:id/share`)
- [x] 7.2 Rotation and expiry UI (expiry display, re-mint, extend); on-demand "Copy link" reveal fetches the key via `/reveal`, never during list rendering
- [ ] 7.3 Email verification flow (verified email only for session restore; still metadata-only for access)
- [ ] 7.4 Usage analytics UI for share links (server `usage` endpoint exists; no owner-facing analytics UI shipped)
- [ ] 7.5 Retention jobs: purge `kb.agent_share_*` rows and end-user data after `retention_days` post-activity
- [ ] 7.6 Unit + gateway tests for each deferred Phase 2 flow (TDD)
- [x] 7.7 Owner session list + read-only transcript: `GET /api/projects/:projectId/share-sessions` (bounded recent-N listing, newest activity first) and `GET /api/projects/:projectId/share-sessions/:id` (project-scoped transcript, 404 cross-project), each enforcing project membership for OAuth callers; gateway chat rail surfaces shared sessions with a last-activity subtitle; unit tests cover the 404 boundary, transcript-not-queried-on-mismatch, message filtering, and membership denial

## 8. Phase 3 — embed / branding / cross-device restore

- [ ] 8.1 Embed/iframe support on the public page (blocked today by `frame-ancestors 'none'`)
- [ ] 8.2 Owner-facing branding (title/logo) on the public page (`welcome_message` shipped; no logo/title theming)
- [ ] 8.3 Cross-device session restore via verified email + `end_user_ref` re-issue

## 9. Verification

All Go commands run from `apps/server/`; gateway checks from `apps/web-ui/`; `openspec` from the repository root.

- [x] 9.1 `go build ./...` clean (server + gateway)
- [x] 9.2 `go test ./...` passes for touched server domains and gateway packages
- [x] 9.3 `go vet ./...` and `golangci-lint run ./...` clean for touched packages
- [x] 9.4 `templ generate` + `task lint` in `apps/web-ui/` clean
- [x] 9.5 `openspec validate agent-public-share` passes
