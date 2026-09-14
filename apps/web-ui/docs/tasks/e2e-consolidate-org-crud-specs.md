# E2E — consolidate overlapping org CRUD specs

**Status:** proposed
**Created:** 2026-09-09
**Source:** [2026-09-09-e2e-ui-driven-mutations](../sessions/2026-09-09-e2e-ui-driven-mutations.md)

## What

`org-create-ui.spec.ts` now drives both halves of the org lifecycle through the UI: create via
the `/orgs/new` form, then delete via the Settings hub danger zone. `org-delete-ui.spec.ts`
("deletes an org via the danger zone") still API-seeds a scratch org and then repeats the same
UI delete path. Decide the fate of the overlap.

## Why

After the UI-first mutation fix, two specs exercise the same danger-zone delete; only the
create setup differs. Either keep `org-delete-ui` for independent failure isolation (it can
fail/run alone without the create form), or fold it into `org-create-ui` and delete the file.

## Depends on

None.

## Notes

- If kept, `org-delete-ui.spec.ts` still seeds its org via `POST /api/orgs` — acceptable SEED
  per the UI-first policy (subject under test is the delete).
- If merged, the long `org-create-ui` test does create + 4 delete steps; a failure mid-delete
  leaves the API `finally` fallback as the cleanup net.
