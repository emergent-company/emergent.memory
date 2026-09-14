# Verify two-line project switcher trigger

**Status:** proposed
**Created:** 2026-09-05
**Source:** [2026-09-05-two-line-project-switcher](sessions/2026-09-05-two-line-project-switcher.md)

## What
Manually verify in the DevTools browser that the project switcher trigger renders correctly as two lines (org name over project name) across states:
- active project with known org (org line + project line)
- active project with unknown org (bare project name only)
- active org with no project (org line + muted "Select project")
- no context (placeholder)

Check long org/project names truncate independently, the two-line block stays within the `btn-sm` height (folder icon + chevron stay vertically centered), and the dropdown menu is unaffected.

## Why
Render tests assert string presence only, not layout. The two-line change is untested visually.

## Depends on
none

## Notes
Trigger markup lives in `gateway/auth_ui.templ` `projectSwitcher` (~lines 194–212). Dev server: `task dev` (ALFRED_PORT default 8095).
