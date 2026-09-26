## Context

The invites domain has two role axes: the invitation role (`org_admin`, `project_admin`, `project_user`, `project_viewer`, validated at create time) and the organization membership role (`org_admin`, `member`). On acceptance, the invitee gains organization membership. Before this change, that membership was always written as `member`, so an `org_admin` invitation under-granted.

## Decisions

- **Role derivation is a pure mapping, not a passthrough.** The organization membership role is derived from the invitation role: `org_admin` → `org_admin`; the project roles → `member`. Only these two values are valid organization membership roles, so any other stored role fails closed rather than being inserted blindly.
- **Fail closed before any write.** The derivation runs before the transaction begins, so an unexpected role grants nothing and leaves the invitation pending.
- **Project membership is unchanged.** A project-scoped invitation already writes `kb.project_memberships` with the invited role; that path is correct and untouched.
- **Spec reconciliation uses remove-and-replace, not modify.** Every legacy requirement's content and most names changed (table, routes, roles, scenarios), so the delta removes the five legacy requirements and adds eight corrected ones with distinct names.
