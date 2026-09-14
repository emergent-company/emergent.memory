# Archive the add-backups-ui OpenSpec change

**Status:** proposed
**Created:** 2026-09-04
**Source:** [2026-09-04-add-backups-ui](../sessions/2026-09-04-add-backups-ui.md)

## What
Finish and archive the `openspec/changes/add-backups-ui/` change now that the feature is implemented and merged:

1. Tick off all `tasks.md` checkboxes (sections 1–4 are done; 4.2's browser test is deferred — mark it as such or leave a note).
2. Move the change under `openspec/changes/archive/` (e.g. `2026-09-04-add-backups-ui/`).
3. Optionally sync the delta spec (`specs/backups/spec.md`) into the main `openspec/specs/` tree if the archive workflow calls for it.

## Why
The implementation shipped (`376cc84`, merged `35ed575`), but the change artifacts still read as unstarted — `tasks.md` has 10 `[ ]` and 0 `[x]`. Leaving it unchecked is spec drift.

## Depends on
- [verify-backups-ui-browser](verify-backups-ui-browser.md) — optionally gate the archive on the manual browser check.

## Notes
- The change depends on `add-auth-project-frame`, which is already merged.
- Design decisions D1–D6 in `design.md` are all implemented as specified (download-no-follow, 404→unavailable, no restore, org-id resolution, `/backups` single page).
