# Manual browser verification of clickable project rows on /orgs

**Status:** done
**Created:** 2026-09-08
**Superseded by:** [2026-09-08-p0-e2e-coverage](../sessions/2026-09-08-p0-e2e-coverage.md)
Superseded by e2e `orgs-row-click-activate.spec.ts`.
**Source:** [2026-09-08-orgs-project-row-click](../sessions/2026-09-08-orgs-project-row-click.md)

## What

On the Organizations page (`/orgs`), click a project row and confirm it opens that project.

## Why

The row was converted from an inert `<div>` to an activate form and unit-tested, but not
browser-verified (no browser access this session).

## Depends on

- none

## Notes

Checklist:

- Each project row under an org is clickable (cursor pointer, hover brightening).
- Clicking a row activates the project and lands on `/agents` (not back on `/orgs`).
- The org header rows and role badges remain non-interactive.
- No regression in the sidebar project switcher (still posts `/projects/activate` and lands
  on the project page).
