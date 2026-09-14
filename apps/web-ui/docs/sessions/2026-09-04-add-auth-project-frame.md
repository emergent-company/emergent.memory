# 2026-09-04 — add-auth-project-frame (Zitadel auth + project/org switching)

## Goal
Implement the `add-auth-project-frame` OpenSpec change: add Zitadel OIDC sign-in, a
signed-cookie session, and project/org switching to the Alfred gateway web UI. Then
fix follow-up issues found in the browser (switcher rendering), group the switcher by
organization, and close the loop with session user identity + token refresh + a sidebar
profile/logout entry.

## Outcome
Done (except deploy-time items). 9 commits landed on `master`:

- `d9d7e74` chore: register `emergent.memory.ui` clone in clonedeps manifest
- `e39e8b1` session foundation — Zitadel config, signed-cookie session, per-request credentials
- `b8a072b` OIDC sign-in, session/key middleware, org/project tenancy + header scoping
- `82cf4cf` login page + project switcher/create UI
- `42df876` docs: mark add-auth-project-frame tasks complete, note ctx-based D3 threading
- `5f2a70f` fix: project switcher — move modal script out of menu, resolve current project in dev mode
- `68d0460` group project switcher by organization
- `8409b9f` session user identity (name/email/picture) + access-token refresh
- `ef77b41` sidebar user profile footer (avatar + sign out)

Auth is **additive and opt-in**: default `AUTH_MODE=dev` preserves the open dev behavior;
`AUTH_MODE=session` enables the full OIDC flow. No behavior change in dev mode.

## Decisions
- Thread per-request credentials via `context.Context` (`sessionContext` resolved inside
  the low-level HTTP helpers) instead of adding `token`/`projectID` params to ~70 method
  signatures — same semantics, far less churn (recorded as a note on design D3).
- `AUTH_MODE` toggle (`dev`|`session`) — ship auth additively; flip the default at deploy time.
- Stateless HMAC-signed session cookie (no server-side session store) — Memory owns all
  durable state; session is signed, HttpOnly, SameSite=Lax.
- OAuth flow state (`state`/`code_verifier`/`nonce`) in a short-lived (10-min) signed cookie
  — consistent with "no server-side store".
- Org is a **grouping header** in the switcher, not an independently switchable context —
  the active unit is the project; picking a project implies its org.
- Avatar = **initials fallback** (Zitadel userinfo has no `picture`; Memory's avatar is an
  S3 `avatarObjectKey` needing a signed URL) — capture `picture` from the id_token opportunistically.
- Forked go-daisy `Sidebar` into `appSidebar` (sidebar_user.templ) — the library component
  has no footer slot, and wrapping it breaks the mobile drawer's `checkbox ~ #_layout-sidebar` CSS.
- Access-token refresh server-side: refresh within a 5-min grace window and recover
  expired-but-authentic sessions via signature-only cookie parse — avoids the ~1h hard logout.

## Changes
- `gateway/config.go` + `config_test.go` — `ZITADEL_*`, `AUTH_MODE` (default `dev`), `SESSION_SECRET`.
- `gateway/session.go` + `session_test.go` — `sessionClaims{access_token,refresh_token,active_project_id,org_id,name,email,picture,exp}`; HMAC issue/verify; `verifySessionSignature` (ignores expiry).
- `gateway/session_context.go` — `sessionContext` (token/project/org/refresh/exp/identity) + ctx helpers.
- `gateway/memory.go` — `tokenFor(ctx)`/`projectIDFor(ctx)` credential resolution; `Org`/`ProjectRef` DTOs; `ListOrgs`/`ListProjects`/`CreateProject`; `sessionHeaders(ctx)` → `X-Project-ID`/`X-Org-ID`.
- `gateway/oidc.go` + `oidc_test.go` — OIDC discovery, `authStart` (PKCE S256 redirect), `authCallback` (state validation + code exchange + id_token decode), `authLogout`, `refreshSessionTokens`.
- `gateway/auth.go` + `auth_refresh_test.go` — `requireSession`/`requireSessionOrKey`/`authDispatch`, `ensureFreshSession` (refresh), `validAPIKey` split.
- `gateway/project_handlers.go` + `project_group.go` — project/org handlers, `groupProjectsByOrg`, `orgNameFor`, `initials`.
- `gateway/auth_ui.templ` + `auth_ui_test.go` — `loginPage`, `projectSwitcher` (org-grouped), `newProjectModal`.
- `gateway/sidebar_user.templ` + `sidebar_user_test.go` — `appSidebar` fork + `userProfileFooter`.
- `gateway/ui.templ`/`ui.go`/`backend.go`/`main.go` — shell/route wiring, `currentUser`/`page()` plumbing.
- `openspec/changes/add-auth-project-frame/tasks.md` + `design.md` — task checkboxes + D3 ctx note.

## Verification
- `go build ./...` (gateway) — clean.
- `go vet ./...` — clean.
- `PATH="/root/go/bin:$PATH" templ generate` — no diff.
- `golangci-lint run ./...` — clean for this change's files (top-level `task lint` shells out to `lefthook`, not installed; ran golangci-lint directly).
- `go test ./...` — this change's tests green. Two failures are pre-existing and unrelated
  (`TestRenderRunPageChatBubbles` / `TestRenderRunPageEmptyTranscript`, in `runs_ui_test.go`,
  reproduce on HEAD before this change). Parallel lanes' in-flight WIP produced transient
  failures during the session (org-members, session-observability, settings-project-sidebar).
- DevTools (`alfred-dev:8095/agents`): login page renders; switcher trigger shows
  `org / project`; grouped dropdown (org header → projects → `New project`); no raw JS blob
  (fixed templ script-in-menu bug); sidebar footer absent in dev mode (no session).

## Open questions / follow-ups
- Deploy-time Zitadel values (`ZITADEL_ISSUER`/client/redirect) come from `emergent-infra` — needed before session mode can go live (spec `13-roadmap`, task `deploy-zitadel-auth`).
- SSO logout (Zitadel `end_session`) is not implemented — local cookie clear only (task `zitadel-sso-logout`).
- Real avatar: initials only for now; resolve Memory `avatarObjectKey` (signed URL) or Zitadel `picture` (task `real-user-avatar`).
- `add-org-members-ui` lane owns the org-members UI and shares the `newProjectModal` create form — watch for a merge conflict (its `TestRenderNewProjectModalOrgSelector` touched the same modal).
- Spec `00-vision`/`09-security`/`10-api-contracts` still describe "single-owner, no browser auth, no tenancy" — synced in this session.

## Tasks
- [deploy-zitadel-auth](../tasks/deploy-zitadel-auth.md) — flip AUTH_MODE + configure Zitadel at deploy time
- [zitadel-sso-logout](../tasks/zitadel-sso-logout.md) — Zitadel end_session SSO logout
- [real-user-avatar](../tasks/real-user-avatar.md) — real avatar (Memory object key / Zitadel picture)
