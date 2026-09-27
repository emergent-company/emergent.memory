## Context

`openspec/changes/unify-scope-authority/` (design in #807, implementation in #812/#829) defines a single app-side entitlement seam and records the org-tier routing refactor as its §4.5 deferred follow-up (design open question 8), and raw `X-Org-ID` trust as open question 10. Issue #1162 is that follow-up. Issue #949 (shipped as `scope-authority-admin-all`) separately narrowed `admin:all` minting to `superadmin_full` only; this change does not touch that decision.

## Goals / Non-Goals

**Goals**

- One shared function decides org-admin authority; every org-scoped decision point consumes it.
- Make raw `X-Org-ID` unusable as a trust source in every posture, closing the `db == nil` trust gap.
- Prove the change can only narrow or preserve authority.

**Non-Goals**

- No change to the platform mint decision (`CanGrantAdminAll`), to the session scope tiers, to role scope sets (#803), or to `GetAllScopes()`.
- No migration: the change is code-only (`kb.organization_memberships` schema is untouched).
- No per-decision project tier is introduced into the org-administration check.

## Decisions

### D1 — Two functions, one seam, no widening

`CanAdministerOrg` is org_admin-of-the-addressed-org only. `CanAdministerOrgOrPlatform` composes it with the shared `superadmin_full` read. Two functions rather than one superadmin-inclusive check is a deliberate fail-closed choice: several org-scoped decision points (org settings mutations, project create/delete/transfer) currently refuse a platform superadmin who is not an org_admin. Routing them through a superadmin-inclusive check would **widen** authority, which this change forbids. `CanAdministerOrgOrPlatform` exists only for the invitation surfaces that already admitted `superadmin_full` (issue #967), so their principal set is preserved exactly.

The check deliberately consults no project tier: a `project_admin` never qualifies (a `project_admin` was never an org-admin authority; adding it would widen).

### D2 — Raw `X-Org-ID` is never a trust source

`RequireAuth` resolves the request org from the declared project's owning org (`kb.projects.organization_id`) only. A header that conflicts with the project's owning org is rejected `403`. A bare header with no resolved project org leaves `user.OrgID` empty — including the `m.db == nil` posture, where the header was previously trusted. The "membership-checked header" variant is rejected too: adding a membership check to the header would create a second trust source for no benefit.

This can only narrow: previously the header populated `user.OrgID` only when `m.db == nil` (never in production, but reachable in the dev/test posture and any future nil-db caller). Every other posture already discarded it. No legitimate production path relied on the header for org context.

### D3 — Scope resolution is project-derived (already true; now pinned)

`resolveOIDCScopes` derives the `org_admin` tier from the declared project's owning org (`lookupProjectOrg`), not from any header, so a spoofed `X-Org-ID` cannot produce an org-admin session entitlement. This change pins that with a regression guard; the header vector that did exist (`user.OrgID` trust) is closed by D2.

## Authority — before / after per routed decision point

| Decision point | Before | After | Widened? |
|---|---|---|---|
| Org rename/delete/member-list (orgs) | `kb.organization_memberships.role == 'org_admin'` for the addressed org | `auth.CanAdministerOrg` (same query/semantics) | No |
| Org tool-settings upsert/delete (orgs) | same | same | No |
| Org-admin invitation create (invites) | role read + `auth.IsSuperadminFull` | `auth.CanAdministerOrgOrPlatform` (`CanAdministerOrg` ∨ `superadmin_full`) | No |
| Invitation accept-time re-check (invites) | bespoke `EXISTS` superadmin + org_admin SQL | `auth.CanAdministerOrgOrPlatform` | No |
| Invitation revoke (invites) | role read + `auth.IsSuperadminFull` | `auth.CanAdministerOrgOrPlatform` | No |
| Project create (projects, REST + MCP) | `kb.organization_memberships.role == 'org_admin'` | `auth.CanAdministerOrg` | No |
| Project delete/restore (projects) | `role == 'org_admin'` in `authorizeProject` | `auth.CanAdministerOrg` | No |
| Project transfer (projects) | `role == 'org_admin'` of the source org | `auth.CanAdministerOrg` | No |
| Request org trust (`RequireAuth`) | project-derived, else raw `X-Org-ID` when `db == nil`, else empty | project-derived, else empty (always) | No — narrowed |
