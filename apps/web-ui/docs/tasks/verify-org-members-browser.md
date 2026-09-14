# Manual browser walkthrough — org/member/invite/profile

**Status:** proposed
**Created:** 2026-09-05
**Source:** [2026-09-05-add-org-members-ui](../sessions/2026-09-05-add-org-members-ui.md)

## What
Run the manual browser test for the `add-org-members-ui` change (OpenSpec task 4.3):
create an org (separate page), create a project under it, add a member (send invite),
accept a pending invite, remove a member, view member details, and edit profile — each
step observable in the DevTools browser.

## Why
The only remaining verification for the change. Could not run in the dev environment
(the dev server points at production Memory `memory.emergent-company.ai` with
`AUTH_MODE=dev`, so there is no authenticated user and exercising the flow would pollute
production).

## Depends on
- a test Memory instance (org/member/invite data) + `AUTH_MODE=session` with a real
  OIDC login.

## Notes
- Maps to task 4.3 in `openspec/changes/add-org-members-ui/tasks.md`.
- DevTools browser runs on the user's local machine (`localhost` resolves there, not the
  dev server).
