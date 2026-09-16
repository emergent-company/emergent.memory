## Context

See `proposal.md` — Why. Alfred's gateway today holds one static `MEMORY_TOKEN` + `MEMORY_PROJECT_ID` and serves the web UI unauthenticated. Emergent Memory (`/root/emergent.memory`, OpenAPI) already implements Zitadel OIDC auth (`/auth/me` "passport-zitadel introspection") and org → project tenancy (`GET/POST /orgs`, `GET/POST /projects`). This change wires Alfred to that existing identity + tenancy surface without touching iOS or the supervisor/bridge workers.

## Goals / Non-Goals

**Goals:**
- Add Zitadel OIDC sign-in + a session cookie; protect the web UI and its `/api` calls.
- Scope the Memory bearer token and active project to the signed-in session.
- Add project list / switch / create (with org context) over Memory's existing endpoints.
- Keep the `X-API-Key` programmatic path working unchanged.

**Non-Goals:**
- No org/member/role management UI, scoped API tokens, data sources, backups, notifications, tasks, or integrations (separate changes).
- No change to the supervisor / bridge workers — they remain single-tenant with the shared server token for this slice.
- No rename (alfred → memory) and no public deployment work in this change.

## Decisions

### D1 — OIDC authorization-code flow with PKCE

The gateway acts as the OAuth client against the Zitadel issuer: redirect to `/authorize`, exchange the code at the token endpoint server-side, validate, and read the user via Memory's `/auth/me` (or `/user/profile`).

- **Why:** authorization-code + PKCE is the secure standard for same-origin web apps; the token exchange stays server-side so the browser only ever sees an opaque session cookie.
- **Alternative rejected:** implicit flow (token in fragment, insecure); client-secret confidential flow (viable, but PKCE removes the need to store a secret in the gateway).

### D2 — Stateless signed-cookie session

The session is an HMAC-signed cookie holding `{ access_token, refresh_token, active_project_id }`, HttpOnly + Secure + SameSite=Lax. No server-side session store.

- **Why:** preserves "Memory owns all durable state / the Go app persists nothing"; works across gateway replicas with a shared signing key.
- **Alternative rejected:** server-side in-memory session map (breaks the persist-nothing principle and multi-replica behavior).

### D3 — Per-request Memory credentials

Refactor `MemoryClient` to take `token` + `projectID` per call (via a request-scoped `sessionContext`) instead of reading struct fields set once at startup.

- **Why:** a single `MemoryClient` instance can serve many sessions; credentials become a parameter, not process-global state.
- **Implementation note:** the `sessionContext` is threaded via `context.Context` (a `withSessionContext`/`sessionContextFrom` pair) and resolved inside the low-level HTTP helpers (`do`/`doH`/`ChatStream`/`mcpInitialize`/`UploadDocument`) rather than as explicit `token`/`projectID` params on every method — this avoids churning ~70 method signatures while keeping the same per-request semantics. The static `m.token`/`m.projectID` remain as the fallback for the API-key/supervisor path.
- **Alternative rejected:** construct a new `MemoryClient` per request (correct, but more allocation and more churn in ~70 call sites).

### D4 — `/api` auth = session OR `X-API-Key`

The API middleware accepts a valid session (→ user token + active project) **or** a valid `X-API-Key` (→ shared server credentials, unchanged iOS behavior). UI page routes require a session.

- **Why:** keeps iOS and any programmatic client working while the web UI moves to session auth.

### D5 — Active project lives in the session cookie

Switching project rewrites the signed cookie's `active_project_id`; all project-scoped Memory paths resolve from the session.

- **Why:** no server-side per-session state; project selection survives reloads and load-balancing.

### D6 — Project scoping via `X-Project-ID` / `X-Org-ID` headers

For session (user-token) calls, the gateway SHALL send `X-Project-ID: <active project>` (and `X-Org-ID` when known) on Memory requests. Confirmed from Memory's `RequireAuth`: a non-`emt_*` (Zitadel) token carries no bound project, so Memory requires the `X-Project-ID` header (`GetProjectID` errors with "x-project-id header required" without it).

- **Why:** the Zitadel access token is user-scoped, not project-scoped; the header is Memory's multi-project selector. The current `emt_*` path needs no header because that token is already project-bound.
- **Alternative rejected:** minting a project-scoped token per switch (unnecessary — Memory already supports header scoping for interactive users).

### D7 — Supervisor / bridge workers stay single-tenant (explicit)

Voice workers keep the shared server token for this slice. Multi-user voice is a follow-up, not silently bundled here.

## Risks / Trade-offs

- **[Access token expiry]** Zitadel access tokens are short-lived → Mitigation: carry the refresh token in the signed cookie and refresh server-side; if refresh fails, force re-login. The cookie's browser `MaxAge` is decoupled from the access token (`SESSION_MAX_AGE`, default 30 days) so the refresh token survives access-token expiry and renews it silently; the signed claims still enforce access-token expiry server-side.
- **[Session cookie = full Memory access]** a stolen cookie grants the user's whole Memory scope → Mitigation: HttpOnly + Secure + SameSite=Lax, short cookie lifetime, logout clears it.
- **[CSRF on state-changing POSTs]** same-origin form posts are CSRF-prone → Mitigation: SameSite=Lax plus a CSRF token on mutating routes.
- **[Public-internet trust boundary]** although deployment is out of scope, the session model must already assume `memory.emergent-company.ai` is public → Mitigation: cookie flags + CSRF + no token in HTML/responses (spec requirement).
- **[New users auto-provision a default org + project]** brand-new users may rarely hit the "no projects" empty state because Memory auto-provisions one → Mitigation: still ship the empty state (orgs with zero projects / deleted projects), but don't treat it as the primary flow.

## Migration Plan

1. Add `ZITADEL_ISSUER`, `ZITADEL_CLIENT_ID`, `ZITADEL_CLIENT_SECRET`, `ZITADEL_REDIRECT_URI` to `Config`; keep `TOKEN_API_KEY` / `MEMORY_*` for the programmatic path.
2. Ship auth as additive first (session middleware + `/api` session-OR-key) behind a config so local dev can still run (`AUTH_MODE=dev` preserves the old no-auth browser path during rollout).
3. Flip the default to session-required once Zitadel is reachable; remove `AUTH_MODE=dev` at the end.
4. Rollback: set `AUTH_MODE=dev` to restore the previous open web UI without reverting code.

## Open Questions

- Exact Zitadel values (issuer URL, client IDs, redirect URI) from `emergent-infra` — needed for deployment, not for the code shape.
- **Resolved (from Memory source):** Memory accepts the Zitadel access token directly as `Authorization: Bearer` — `validateToken` introspects it (RFC 7662) or falls back to `/oidc/v1/userinfo`, then auto-provisions the user profile. No `emt_*` exchange step is required.
