## Why

`unify-scope-authority` (design merged in #807, implemented by #812/#829) established one app-side entitlement seam for platform-scoped token minting (`pkg/auth.CanGrantAdminAll`) but **deferred** the wider routing of org-scoped decision points (task §4.5, design open question 8). Today the same org-admin authority is re-derived independently in four places:

- `domain/orgs` — `requireOrgAdmin` compares `kb.organization_memberships.role` to the literal `"org_admin"` (rename, delete, member listing, org tool settings).
- `domain/invites` — `Handler.mayAdministerOrg` composes a role read with `auth.IsSuperadminFull`, and `Service.inviterIsOrgAdmin` carries its own raw `EXISTS` SQL over `core.superadmins` + `kb.organization_memberships`.
- `domain/projects` — `AuthorizeOrgAdmin` and `Transfer` compare the role string locally; `authorizeProject`'s `accessOrgAdmin` branch does too.

That duplication is exactly the drift class the `authz-model` reference (Rule 3) warns about: a check for one resource family re-implemented per entrypoint or per domain.

The same follow-up must also fix raw `X-Org-ID` trust (design open question 10): org context must be **route/project-derived only**, and a bare client `X-Org-ID` — including a "membership-checked header" variant — must be **rejected** as a trust source. `RequireAuth` still assigned the raw header to `user.OrgID` in the no-database posture, so a client could inject an arbitrary org context into every handler that reads `user.OrgID`.

## What Changes

- **One shared org-administration decision.** `pkg/auth/entitlement.go` gains `CanAdministerOrg(ctx, db, orgID, userID)` — the single check for org-admin authority over an organization (`kb.organization_memberships`, `role = 'org_admin'`), fail-closed, and deliberately *without* a platform-superadmin or project arm so it can never widen a currently org-admin-only decision point. `CanAdministerOrgOrPlatform` composes it with the shared `superadmin_full` seam for the two invitation surfaces that already admitted a platform superadmin (issue #967); it adds no second membership query.
- **Route the remaining decision points through it.** Org settings mutations, org member invitation (create + accept-time re-check + revoke), and project create/delete/transfer within an org consume the shared seam instead of local role comparisons or bespoke `EXISTS` SQL.
- **Reject raw `X-Org-ID` as a trust source.** `RequireAuth` no longer assigns the header to `user.OrgID` when no trusted project-derived org context exists; the org is left empty (fail closed) in every posture. The declared project's owning org remains the one route-derived source. A header that conflicts with the project's owning org is still rejected `403`.

This change **only narrows or preserves** authority: every routed decision point keeps its exact before/after principal set (org settings and project routes still refuse a platform superadmin who is not an org_admin; invitation surfaces still admit an active `superadmin_full`). No new grant is introduced.

## Capabilities

### Modified Capabilities

- `scope-authority`: adds the route-derived organization-context requirement (no raw `X-Org-ID` trust) and the shared organization-administration decision requirement consumed by every org-scoped decision point.

## Impact

- `apps/server/pkg/auth/entitlement.go` — new `RoleOrgAdmin`, `CanAdministerOrg`, and `CanAdministerOrgOrPlatform`.
- `apps/server/pkg/auth/middleware.go` — `RequireAuth` fails closed instead of trusting the raw `X-Org-ID` header when no project-derived org exists.
- `apps/server/domain/orgs/{repository,service,tool_settings_service}.go` — `IsOrgAdmin` delegates to the shared seam; `requireOrgAdmin` consumes it.
- `apps/server/domain/invites/{handler,service}.go` — `mayAdministerOrg` and `inviterIsOrgAdmin` consume the shared seam.
- `apps/server/domain/projects/service.go` — `AuthorizeOrgAdmin`, `Transfer`, and `authorizeProject`'s org-admin branch consume the shared seam.
- Tests in `pkg/auth`, `domain/orgs`; no config, schema, migration, or API-surface change.
