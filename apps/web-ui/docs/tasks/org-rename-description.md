# Organization rename + description

**Status:** done (name-only)
**Created:** 2026-09-05
**Source:** [2026-09-05-org-context-picker](../sessions/2026-09-05-org-context-picker.md)

## What
Add the ability to rename an organization (and optionally set a description/logo). Requires a new Memory endpoint (e.g. `PATCH /api/orgs/{id}`) plus a gateway `MemoryClient` method and a rename control on the org context (tool-settings page or a dedicated org-settings page).

## Why
The Memory `Org` entity is name-only and immutable via the current API (create + delete only, no update). The org context sidebar exposes Projects/Members/Tool settings/Delete, but rename was the one missing org-config primitive and was deferred this session.

## Depends on
- Memory backend: add an org update endpoint (and description field if desired).

## Notes
- Org tool settings already exist (`/api/admin/orgs/{orgId}/tool-settings`) and are surfaced in the gateway — rename is the remaining gap.
- Consider whether rename belongs on the existing org tool-settings page or a new org-settings page in the org context.

## Done (2026-09-10)

Shipped **name-only rename**. The `description`/`logo` half of this task is
**deferred**: the Memory `Org` entity has no description (or logo) column, and
adding one requires a DB migration. Scope was held to name only; a follow-up
task should add the column + migration first.

What landed:

- **Memory** — `PATCH /api/orgs/{id}`, body `{"name":"..."}`, auth required.
  - `200` → `OrgDTO` `{"id","name"}`.
  - `400` invalid name (trimmed non-empty, ≤120 chars).
  - `404` unknown or soft-deleted org.
  - `401`/`403` per existing org auth middleware.
  - New `UpdateOrgRequest`, `Repository.UpdateName` (sets `kb.orgs.name` +
    `updated_at = NOW()`, respects `deleted_at IS NULL`), `Service.Update`, and
    `Handler.Update`; route registered as `g.PATCH("/:id", h.Update)`.
- **Gateway** — `MemoryClient.UpdateOrg(ctx, id, name)` calls the PATCH with the
  same session headers as the other org calls. The rename control lives in a new
  **General** section of the org Settings hub
  (`GET /orgs/:id/settings/general`, `POST /orgs/:id/rename`), added to the
  settings sub-nav. The form is a native PRG form (`hx-boost="false"`) and
  surfaces failures via the standard `?err=` flash; success redirects with
  `?renamed=1`.

Tests: Memory repository (real-Postgres, skips when unavailable), service and
handler tests (success / invalid name / unknown id). Gateway: org-context tests
for the form render, rename success, local empty-name guard, and backend error.
