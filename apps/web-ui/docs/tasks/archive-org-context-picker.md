# Archive org-context-and-project-picker change

**Status:** proposed
**Created:** 2026-09-05
**Source:** [2026-09-05-org-context-picker](../sessions/2026-09-05-org-context-picker.md)

## What
Finish the two remaining manual-browser tasks in the OpenSpec change, then archive it:
1. `4.5` — confirm the picker renders full-width with no horizontal overflow at a mobile viewport.
2. `6.5` — interactive smoke pass: picker scroll, org switch via header/cogwheel, no-org wizard, last-used auto-select.

Then `openspec archive org-context-and-project-picker` (and sync the delta specs to the main spec tree if the workflow requires it).

## Why
The change is implemented and covered by automated tests, but the two `tasks.md` items that require a real browser (and session-mode auth for org-context flows) are still unchecked. Dev mode has no session, so org-context flows can't be exercised without a session-mode deployment.

## Depends on
- A session-mode deployment (`AUTH_MODE=session`) reachable from a browser.

## Notes
- Reconcile overlap with the in-flight `add-auth-project-frame` and `add-org-members-ui` changes (both touch org/project grouping) when their specs sync.
- Confirm `groupProjectsByOrg` empty-org + nil-on-zero-projects behavior is reflected in the synced spec.
