# Member role editing on the member details page

**Status:** done
**Created:** 2026-09-05
**Source:** [2026-09-05-add-org-members-ui](../sessions/2026-09-05-add-org-members-ui.md)

## What
Extend the read-only `/members/:userId` details page (added in `add-org-members-ui`)
with role management, so an authorized user can change a member's role from the details
page.

## Why
The details page was intentionally built read-only ("for now we just need to see the
member's details"). Role adjustment is the natural next step.

## Depends on
- none

## Notes
- Memory has **no member-role PATCH** endpoint — see design D2 in
  `openspec/changes/add-org-members-ui/design.md`. A role "change" is therefore
  **remove + re-invite** (remove member, re-invite the email with the new role). Do not
  fake a mutation the server cannot persist.
- Role enums: `project_admin` / `project_user` (project level), `org_admin` (org level).
- Last-admin removal is blocked server-side (403 `last-admin`).

## Done (2026-09-10)

Implemented on the member details page as **remove + re-invite**, since Memory still has
no member-role PATCH (design D2). No fake mutation is written.

- **UI** (`gateway/org_members_ui.templ`): a new "Role" section shows the current role
  and, for a removable member, a change-role form (project role select + "Change role").
  Submitting confirms `window.confirm` text that spells out the consequence: *"the member
  is removed and re-invited with the new role; they must accept the new invitation to
  regain access."* The form sets `hx-boost="false"` so it runs a native PRG submit. When
  the member is the project's sole admin the form is replaced by an assign-another-admin
  hint (mirrors the existing remove affordance; `canRemoveMember`).
- **Handler** (`gateway/org_members_ui.go`): new `POST /members/:userId/role`
  (`uiChangeMemberRole`) reads the member's email and role server-side from
  `ListMembers` (never trusts a form address), validates the target is a distinct
  supported project role, then calls the shared `changeMemberRole` helper. That helper
  reuses the same remove and invite primitives as the existing flows
  (`removeProjectMember` → `s.memory.RemoveMember`; `createProjectInvite` →
  `s.memory.CreateInvite`) sequenced remove → invite for the member's own email. The
  members-page remove handler and the invite form were refactored onto those helpers too.
- **Failure honesty**: invalid / unchanged role is rejected before any mutation;
  last-admin removal is mapped to *"cannot change the last admin's role; assign another
  admin first"* with no invite; a failed re-invite after a successful removal returns
  *"member removed, but the new … invite failed …"* (explicit partial failure, no success
  flash). Success redirects to `/members?role-changed=1` with a flash making clear the
  invite still has to be accepted. Errors redirect back to the member's details page and
  surface via the existing `?err=` flash.
- **Tests** (`gateway/org_members_ui_test.go`): details page shows current role + change
  control; sole-admin suppression + PRG error render; route flow asserts remove-then-invite
  with the member's email and new role; invalid / unchanged role; last-admin 403 surfaces
  an error and leaves no success or invite; re-invite-after-removal partial failure;
  unknown member.
- **Spec**: added a "Change a project member's role" requirement to the org-membership
  change spec and updated design D2 to describe the remove + re-invite affordance.
