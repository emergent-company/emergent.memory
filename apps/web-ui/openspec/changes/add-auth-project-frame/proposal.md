## Why

Alfred is single-owner and single-project by construction: the gateway holds one static `MEMORY_TOKEN` + `MEMORY_PROJECT_ID`, the web UI serves unauthenticated (dev mode), and there is no way to switch between or create projects. Emergent Memory already implements the full tenancy model — Zitadel OIDC users → orgs → projects — but ships no web UI. To become Memory's web console, Alfred needs its first slice: let a user sign in with their Memory account and switch/create projects.

## What Changes

- Add a Zitadel OIDC sign-in to the gateway (authorization-code flow) and issue a session cookie; the web UI and its `/api` calls become session-authenticated instead of open.
- Make the Memory bearer token **per-session** (the signed-in user's token) instead of the single static `MEMORY_TOKEN`; make the active project **per-session** instead of the static `MEMORY_PROJECT_ID`.
- Add project list / switch / create — proxying Memory's `GET /projects` and `POST /projects` (with `GET /orgs` to supply the `orgId` required on create).
- Keep the existing shared `X-API-Key` path working for programmatic/iOS clients alongside session auth, so this slice does not break iOS.
- **BREAKING (dev-mode web UI):** the browser UI no longer renders without a signed-in session; the previous "no browser auth" dev mode is retired.

## Capabilities

### New Capabilities
- `user-auth`: Zitadel OIDC sign-in, session cookie, and a session-scoped Memory token; the web UI and API require a session.
- `project-management`: list, switch, and create Memory projects, with org context for creation.

### Modified Capabilities
- `agent-memory-api`: the "Authenticated access" requirement broadens from shared API key only to session (web) or API key (client).
- `session-log-api`: the "Authenticated access" requirement broadens the same way.

## Impact

- `gateway/auth.go`: OIDC middleware + session cookie (replaces/augments `requireAPIKey`).
- `gateway/config.go`: new env — `ZITADEL_ISSUER`, `ZITADEL_CLIENT_ID`, `ZITADEL_CLIENT_SECRET`, `ZITADEL_REDIRECT_URI`.
- `gateway/memory.go`, `gateway/settings.go`, `gateway/backend.go`: thread token + projectID per-request; add `ListProjects`, `CreateProject`, `ListOrgs`.
- `gateway/main.go`, `gateway/ui.go` + templ: login page, project switcher, route gating.
- Session storage: signed cookie carrying the user's Memory bearer token, held server-side only; no durable state added (Memory remains the sole durable store).
- iOS / `X-API-Key` programmatic path unchanged in this slice.

### Open question (resolved)

Memory accepts the Zitadel access token directly as the `Authorization: Bearer` token (introspects it, or falls back to `/oidc/v1/userinfo`) — no `emt_*` exchange needed. Confirmed from `emergent.memory` source (`apps/server/pkg/auth/middleware.go`). The remaining Zitadel specifics (issuer URL, client IDs, redirect URI) come from `emergent-infra` at deploy time.

### Out of scope (separate changes)

Rename alfred → memory; deployment under `memory.emergent-company.ai`; org/member/role management UI; scoped API tokens; data sources, backups, notifications, tasks, integrations.
