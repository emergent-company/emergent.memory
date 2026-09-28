## Context

The scope vocabulary (`ValidApiTokenScopes` in `domain/apitoken/entity.go`) has 23 user-mintable scopes: coarse `schema/data/agents/projects` read/write + `chat:use`, fine-grained `graph/branches/journal/skills/documents` read/write + `search` + `schema:migrate`, and the two platform scopes `admin` / `admin:all`. `ScopeImplies` (`pkg/auth/middleware.go`) is the single canonical umbrella-expansion table. The platform tier is already hardened: `admin` and `admin:all` are minted only by a `superadmin_full` (`checkPlatformScopeGrant` → `CanGrantAdminAll`), per the `scope-authority-admin-all` change.

Project membership roles are `project_admin` / `project_user` / `project_viewer`, mapped to nested scope sets in `pkg/auth/scope_mapping.go`. Member list/remove already exist (`GET`/`DELETE /api/projects/:id/members/:userId`, `domain/projects`), gated through `authorizeProject(..., accessProjectAdmin)` which admits a `project_admin` or an `org_admin` of the owning org.

## Goals / Non-Goals

**Goals:**

- A project-scoped admin umbrella (`project:admin`) whose expansion is exactly the project tier, so a CI/automation principal can administer one project without a hand-assembled scope list and without platform reach.
- An in-place role change (`PATCH …/members/:userId`) that preserves the invite/remove vocabulary, protects the last admin, and revokes tokens on downgrade.
- A Members section under Project Settings and a `project:admin` scope-picker option visible only to project admins.

**Non-Goals:**

- No change to the platform tier (`admin` / `admin:all`) — that stays `superadmin_full`-only.
- No `mcp:admin`, `org:*`, or `project:invite:create` reach on any project-bound token.
- No change to the existing list/remove member server contract (PII, stats).

## Decisions

### 1. Why a dedicated `project:admin` scope instead of loosening `admin` / `admin:all`

`admin:all` is a platform umbrella: its `ScopeImplies` expansion includes `mcp:admin`, the whole `org:*` family (`org:read`, `org:invite:create`, `org:project:create`, `org:project:delete`), `project:invite:create`, and the `discovery:*` / debug scopes. Granting a project admin `admin:all` would be escalation: it buys cross-tenant and org-administration power, not just one project's administration. And `admin:all` minting is already `superadmin_full`-only, so loosening it would reopen a closed class.

The alternative — binding an existing umbrella to a project — is unsound because project binding is not enforced on every route (an `emt_*` token may address any project unless a route opts into `RequireProjectTokenScope`), so a "project-bound `admin`" could still reach admin surfaces whose route does not check the binding.

A dedicated `project:admin` scope avoids both failure modes: its expansion is a flat, explicit project-tier list with **no** platform / `mcp:admin` / `org:*` / `project:invite:create` / `discovery:*` members, so even a route that skips the binding check can only reach project surfaces. The platform tier remains untouched.

### 2. Mint rule for `project:admin`

`project:admin` is mintable **only on a project-bound token** (`project_id IS NOT NULL`), **only** by a caller who is a `project_admin` of the addressed project **or** an `org_admin` of the project's owning organization (`kb.projects.organization_id`). The gate is `checkProjectAdminScopeGrant` (`domain/apitoken/service.go`), backed by `CanGrantProjectAdmin` (`domain/apitoken/repository.go`), a local query so the apitoken domain does not import the projects domain (cycle). Account-level tokens (`project_id IS NULL`) are refused with `403 project-admin-scope-denied`.

Deliberate consequence: a `superadmin_full` who is neither a `project_admin` of the project nor an `org_admin` of its owning org is **not** a `project:admin` mint path — the scope is project-tier, and platform authority does not flow down into project-tier minting. Platform principals mint `admin` / `admin:all` instead.

### 3. Role-change semantics: in-place vs remove + re-invite

The change is **in-place** (`PATCH …/members/:userId` mutates the existing `kb.project_memberships.role`), not a remove-and-re-invite. Rationale: a member is already authenticated, identity-linked, and provisioned; removing them tears down membership (and any invitation-derived state) and re-inviting re-runs the email + accept flow, which is both noisy and lossy (joined-at, token history). The role vocabulary is exactly `project_admin | project_user | project_viewer` (validated by `roleRank`, unknown → `400 invalid-role`).

Two guards:

- **Last-admin protection** — demoting the sole `project_admin` (admin → user/viewer) returns `403 last-admin`, mirroring the remove-member guard.
- **Downgrade revocation** — a rank decrease (`roleRank(new) < roleRank(old)`, i.e. admin → user/viewer or user → viewer) revokes the member's project-scoped tokens (`RevokeByProjectAndUser`) so stale privileges cannot outlive the role change. Upgrades and no-ops revoke nothing. Revocation failure is non-fatal (logged; the role change stands).

## Risks / Trade-offs

- **`project:admin` is a large umbrella.** A leaked `project:admin` token can read/write/delete within one project. Mitigation: it is mintable only by a project/org admin, and the flat expansion excludes every platform/org scope, so the blast radius is bounded to the one project.
- **Escalation via org_admin.** An `org_admin` of the owning org can mint `project:admin` for any project in that org. This is intentional (org_admin is the project's admin-of-last-resort, matching the member-remove/role-change gate) and is bounded by the project tier only.
- **In-place role change vs invitation role.** A role changed in place bypasses the invitation role-grant authorization. This is fine: the caller is already `project_admin`/`org_admin`-gated (`accessProjectAdmin`), which is at least as strong as the invitation path's authority check.
- **PII.** The server list-members endpoint returns email to any project member. The web members section redacts non-admin PII at the gateway layer (see `web-project-settings-members`); the server contract is unchanged.

## Migration Plan

- Additive only: one scope string + expansion entries, one `PATCH` route + service method, new gateway routes/nav entry/page, and a scope-picker option. No schema migration (role values and membership rows are unchanged).
- Deploy via the normal gateway build; rollback is a revert.
