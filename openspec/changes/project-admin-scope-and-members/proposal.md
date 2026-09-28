## Why

Project administration is today spread across two authority shapes: a human role (`project_admin` membership, granted via invitation) and a token scope vocabulary that has no project-scoped admin umbrella. A CI or automation principal that needs to administer one project's data, schema, agents, graph, branches, journal, skills, documents, search, and chat must be minted a hand-assembled scope list, and any project-admin action is out of reach of a token entirely. Separately, a project member's role can only be changed by removing and re-inviting them — there is no in-place role change, and no token revocation on downgrade.

## What Changes

- Add a project-level token scope `project:admin`: a project-bound admin umbrella that expands to every project scope (data / schema / agents / graph / branches / journal / skills / documents / search / chat) but NOT the platform tier (`admin` / `admin:all`), `mcp:admin`, or any `org:*` scope. It is mintable only on a project-bound token, only by a `project_admin` of that project or an `org_admin` of the project's owning organization. Account tokens never carry it. Platform `admin` / `admin:all` stay `superadmin_full`-only.
- Add an in-place role change endpoint `PATCH /api/projects/:id/members/:userId` (`project_admin | project_user | project_viewer`), gated to `project_admin` / `org_admin`, with last-admin protection and token revocation on downgrade.
- Add a Web UI Members section under Project Settings (`/settings/members*`) with a sidebar/settings-nav entry, and offer `project:admin` in the token scope picker to project admins only.

## Capabilities

### New Capabilities
- `web-project-settings-members`: web UI to list/invite/remove/change-role of project members, gated to project admins, with last-admin protection and no PII to non-admins.
- `project-membership-management`: the `PATCH /api/projects/:id/members/:userId` role-change contract (authority, role vocabulary, last-admin guard, downgrade revocation).

### Modified Capabilities
- `scope-authority`: the project tier MAY additionally mint `project:admin` on a project-bound token; the platform tier is unchanged (`superadmin_full`-only).

## Impact

- `apps/server/pkg/auth/` — `ScopeImplies["project:admin"]` (flat project-scope umbrella, no platform/org scopes).
- `apps/server/domain/apitoken/` — `project:admin` in the valid-scope vocabulary and oneof tags; mint gate (`checkProjectAdminScopeGrant` + `CanGrantProjectAdmin`); account-token denial.
- `apps/server/domain/projects/` — `PATCH /api/projects/:id/members/:userId` handler + `UpdateMemberRole` service (role validation, last-admin guard, downgrade token revocation).
- `apps/web-ui/gateway/` — `/settings/members*` routes, settings-nav + sidebar entry, members page, and the `project:admin` option in the token scope picker (admin-only).
- `docs/security/authz-model.md` — document the `project:admin` scope and the member-role PATCH endpoint.

## Scope

- **In scope**: the `project:admin` scope (mint + expansion), the member role-change endpoint, the Web UI members section and scope-picker option.
- **Out of scope**: loosening the platform `admin` / `admin:all` tier; `mcp:admin` or `org:*` on any project-bound token; token expiry management; email/PII changes to the existing list-members server contract.
