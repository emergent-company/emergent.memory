# Enforce object-level authorization on project delete/restore

**Status:** proposed
**Created:** 2026-09-10
**Source:** Independent review of the project-pending-deletion change (org-landing delete/restore flow)

## What
The Memory service's project delete/restore endpoints have no object-level
authorization. Both are gated only by authentication plus a coarse API-token
scope check, so any authenticated caller who knows a project id can act on a
project they do not belong to:

- `DELETE /api/projects/:id` — **pre-existing**, deferred during the
  pending-deletion change.
- `POST /api/projects/:id/restore` (the new "Cancel deletion" endpoint) —
  inherits the same gap.

Memory paths: `apps/server/domain/projects/routes.go` (route + handler),
`pkg/auth/middleware.go` (`RequireAuth`, `RequireAPITokenScopes`).

Concretely:

1. Both routes are protected by `RequireAuth` + `RequireAPITokenScopes("projects:read")`.
2. `RequireAPITokenScopes` is bypassed for OAuth (browser session) callers, so a
   signed-in user can delete or restore **any** project id — no membership and no
   `org:project:delete` scope required.
3. Neither handler verifies the actor is an `org_admin` of the project's org (or
   a project member with an appropriate role) before mutating.
4. Restore accepts a `projects:read`-scoped API token — the weakest scope — for a
   destructive mutation, rather than requiring a delete/write scope.

## Why
`DELETE /api/projects/:id` and `POST /api/projects/:id/restore` are destructive,
cross-tenant operations. Relying on an unguessable id and a read scope is
tenant-isolation by obscurity: a session user in the shared multi-tenant service
(or a read-scoped token) can schedule or cancel deletion of another org's
projects. The gateway's UI is not a security boundary — users can call the
Memory API directly. This is a pre-existing hole for delete; the pending-deletion
feature adds a second endpoint with the same weakness.

## Proposed fix
In Memory (`apps/server/domain/projects/routes.go` unless noted):

1. **OAuth-aware scope check.** Stop relying on `RequireAPITokenScopes` alone.
   Add/extend an auth helper in `pkg/auth/middleware.go` so an OAuth (session)
   caller is also required to hold the `org:project:delete` scope (or the
   equivalent org/project permission), instead of the middleware silently
   passing sessions through. API-token callers must present
   `org:project:delete` too.
2. **Actor membership/role check.** After resolving the target project, load the
   actor's membership for the project's org and require `org_admin` (or the
   project-member role the policy designates as permitted) before mutating.
   Reject with `403` and no state change when the actor is not authorized.
3. **Restore rejects read-scoped tokens.** Require a delete/write scope for
   `POST /api/projects/:id/restore`; a `projects:read`-only token must be
   rejected (restore is a mutation, not a read).
4. **Tests.** Add handler/middleware tests covering the authorization matrix:
   - OAuth session, non-member / non-admin → `403` (DELETE and restore).
   - OAuth session, `org_admin` of the project's org → allowed.
   - API token with `org:project:delete` → allowed.
   - API token without the scope, or `projects:read`-only → `403` (both; restore
     explicitly rejects read-scoped tokens).
   - Unknown project id → `404` (unchanged), and no mutation on any rejected path.

## Acceptance criteria
- [ ] `DELETE /api/projects/:id` requires `org:project:delete` for **both** OAuth
      sessions and API tokens — the OAuth bypass is closed.
- [ ] `POST /api/projects/:id/restore` requires `org:project:delete` (or a
      designated delete/write scope) and **rejects** `projects:read`-scoped
      tokens.
- [ ] Both handlers verify the actor is `org_admin` of the project's org (or a
      project member with the permitted role); unauthorized actors get `403` and
      no state change.
- [ ] Authorization failures are covered by tests (matrix above), including the
      OAuth-session case that `RequireAPITokenScopes` currently skips.
- [ ] Existing authorized delete/restore behavior (gateway UI flows) is unchanged.

## Depends on
- Pending-deletion feature (`POST /api/projects/:id/restore`) merged/deployed to
  the Memory service.

## Notes
- Gateway callers: `gateway/memory.go` (`DeleteProject`, `RestoreProject`) send
  the user/service token through `pkg/auth`; no gateway-side change should be
  required beyond surfacing the new `403` as an error flash.
- Confirm the exact scope name (`org:project:delete`) and the permitted
  project-member role against the Memory auth model before implementing; the
  gateway UI currently gates the Transfer/Delete affordances on `org_admin` of
  the source org (`transferState` in `gateway/org_context.go`), which is a
  reasonable reference for the server-side rule.
- Deferred from the project-pending-deletion change; pre-existing for `DELETE`.
- Shipped feature: [2026-09-10-project-pending-deletion](../sessions/2026-09-10-project-pending-deletion.md)
  (memory PR #421, `POST /api/projects/:id/restore` now live on `main`).
