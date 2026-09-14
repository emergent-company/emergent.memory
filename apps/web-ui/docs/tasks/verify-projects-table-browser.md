# Manual browser verification of the org-landing projects table

**Status:** done
**Created:** 2026-09-08
**Superseded by:** [2026-09-08-p0-e2e-coverage](../sessions/2026-09-08-p0-e2e-coverage.md)
Superseded by e2e `projects-table-ui.spec.ts` (bulk + row-menu delete, Open activation).
**Source:** [2026-09-08-org-projects-table](../sessions/2026-09-08-org-projects-table.md)

## What

Walk through the org landing page (`/orgs/:id`) in the browser and confirm the projects
table behaves correctly.

## Why

The feature was built and unit-tested but not browser-verified (the app is served on the
tailnet tunnel and this session had no browser access). The row menu went through several
positioning iterations, so the interaction needs a real eyeball pass.

## Depends on

- none

## Notes

Checklist:

- Select-all toggles every row checkbox and updates the bulk-delete count.
- Bulk "Delete selected" posts the selected ids and redirects with `?deleted=N` flash.
- Per-row 3-dot menu opens at bottom-left of the trigger, no position jump, no clipping by
  the table/card overflow — including the **last row** (menu should flip above if needed).
- Only one menu open at a time; clicking outside or pressing Esc closes it.
- "Open" activates the project (lands on `/agents`), "Delete" prompts confirm then deletes.
- Empty org still shows the create CTA.
