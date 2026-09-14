# Org-level invitations (org_admin role)

**Status:** done
**Created:** 2026-09-05
**Done:** 2026-09-10
**Source:** [2026-09-05-add-org-members-ui](../sessions/2026-09-05-add-org-members-ui.md)

## What
Allow inviting a person to an **organization** (not just the active project), carrying
the `org_admin` role, from the organizations view.

## Why
The invite flow shipped in `add-org-members-ui` is project-scoped only
(`project_admin` / `project_user`), and the org-level invite path was deferred.

## Depends on
- none

## Notes
- `CreateInviteDto` already accepts an `orgId` and the `org_admin` role; the gap is a
  UI affordance (on `/orgs` or the orgs list) that sends `orgId` + role `org_admin`
  without a `projectId`.
- Invite role enum: `org_admin`, `project_admin`, `project_user`.

## Done (2026-09-10)
- `/orgs` access tree: `org_admin` rows carry an "Invite" action linking to a new
  standalone page `GET /orgs/:id/invite` (`OrgInvitePage`, `org_context.templ`).
- The form posts to `POST /orgs/:id/invite` (`uiOrgInviteCreate`) with a hidden
  `orgId` + the fixed `org_admin` role and **no projectId**; it opts out of
  hx-boost (`hx-boost="false"`) for a native POST/PRG.
- Server-side role validation accepts only `org_admin`; success redirects to
  `/orgs/:id/members?invited=1` (members page now renders PRG flashes), failures
  go back to the invite page with `?err=`. Memory rejections surface honestly —
  no faked success.
- Spec updated (`docs/spec/04-go-application.md`); render + handler tests added in
  `org_members_ui_test.go` / `org_context_test.go`.
