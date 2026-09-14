# 2026-09-09 — E2E spec grouping + UI-driven mutations

## Goal

- Make the Playwright UI tree navigable: organize ~38 flat spec files into domain groups.
- Enforce the policy that mutations under test run through the real UI, not raw API
  (`page.request.*`), because a UI-driven test exercises the actual interaction.

## Outcome

Done. Four commits, all pushed to `master`:

- `97059d1` — specs restructured into 11 surface folders; multi-flow files describe-wrapped.
- `b274d13` — `org-create-ui` deletes via the UI danger zone, not a bare `POST /orgs/:id/delete`.
- `d4d9e76` — `agent-create-ui` creates via the real "New agent" modal, not `POST /api/agents`.
- `ab81249` — dropped the unused `activateProject` helper.

Full audit of all 39 `page.request.*` call sites: the only true mutation-under-test
violation was `agent-create-ui`; everything else was legitimately SEED / CLEANUP / VERIFY.

## Decisions

- **Group specs by feature surface into subfolders of `tests/e2e/specs/`** — Playwright's UI
  tree turns directories into top-level folders; a flat 38-file list was the navigation pain.
  Folders: organizations, projects, agents, skills-schedules, objects, schema, documents,
  sessions, settings, account, shell.
- **Wrap only multi-flow files in `test.describe`** — a per-file describe on single-flow files
  adds a redundant nesting level (file folder already names the domain). Existing surface
  describes (e.g. "Org context pages", "Spotlight palette") kept verbatim.
- **`page.request` allowed only as SEED / CLEANUP / VERIFY** — a spec whose title claims a
  create/edit/delete must drive the form. VERIFY GETs are stronger than DOM-only assertions.
- **Keep API delete as a `finally` fallback** in UI-delete tests — if an assertion fails before
  the UI deletion completes, dev memory still doesn't accumulate entities.
- **Diagnosed Playwright UI "Loading…" forever** as a stale `ws` GUID in the bookmarked
  `uiMode.html?ws=…` deep URL (server regenerates the GUID each start; old ones 404 the WS
  upgrade). Fix is entrypoint-level: bookmark the root URL, which 302-redirects to a fresh
  GUID. No code change warranted.

## Changes

- `tests/e2e/specs/*.spec.ts` → moved into 11 surface folders (`git mv`); helper imports
  deepened `'../helpers'` → `'../../helpers'`.
- `tests/e2e/specs/{projects/projects-table-ui, agents/agent-model-warning-ui,
  objects/object-create-ui, documents/document-upload-ui, account/account-menu-ui,
  organizations/org-settings-hub, organizations/orgs-row-click-activate}.spec.ts` — wrapped in
  a top-level `test.describe`.
- `tests/e2e/specs/organizations/org-create-ui.spec.ts` — delete moved into the test body via
  Settings hub → Danger zone → `Delete organization` (accept `confirm()` dialog), assert
  redirect + name gone; API delete demoted to `finally` fallback gated on `deletedViaUi`.
- `tests/e2e/specs/agents/agent-create-ui.spec.ts` — creates via `/agents` → "New agent" modal
  (`#agent-name` fill, submit), then grabs `/agents/:id` from the reloaded list row link;
  detail-page smoke unchanged; header comment corrected (UI form was *not* covered elsewhere).
- `tests/e2e/specs/organizations/orgs-row-click-activate.spec.ts` — removed the never-invoked
  `activateProject` helper.
- `docs/spec/12-operations.md` — E2E section corrected to reality (live dev gateway + real
  tenant, project split, surface-folder layout, UI-first mutation policy).

## Verification

- `cd tests/e2e && npx playwright test --list` — clean, 68 tests / 38 files (after restructure).
- `npx playwright test specs/organizations/org-create-ui.spec.ts --project=mutations` —
  **2 passed (10.6s)** (setup + test), live against the dev gateway.
- `npx playwright test specs/agents/agent-create-ui.spec.ts --project=mutations` —
  **2 passed (20.3s)**. Setup emits a benign provider-seed skip (catalog unsynced / 401) —
  non-blocking by design.
- `npx playwright test specs/organizations/orgs-row-click-activate.spec.ts --list` — clean.
- `git status` clean at end of session.

## Open questions / follow-ups

- Playwright UI deep URLs are unstable across server restarts (stale `ws` GUID). Consider
  documenting the root-URL entrypoint in `tests/e2e/README.md` so it stops recurring.
- `org-delete-ui.spec.ts` (API-seeds its org, UI-deletes) now overlaps the delete half of
  `org-create-ui.spec.ts` (UI-create + UI-delete). Decide whether the standalone danger-zone
  spec stays (independent failure isolation) or merges into `org-create-ui`. See task
  [e2e-consolidate-org-crud-specs](../tasks/e2e-consolidate-org-crud-specs.md).
- `agent-model-warning-ui.spec.ts` still shapes its "explicit model" fixture via API PUT/POST
  (subject is alert rendering, not editing) — could drive the settings model select instead
  for harder coverage. See task
  [e2e-model-warning-fixture-via-ui](../tasks/e2e-model-warning-fixture-via-ui.md).

## Tasks

- [e2e-consolidate-org-crud-specs](../tasks/e2e-consolidate-org-crud-specs.md) — decide org delete-spec overlap.
- [e2e-model-warning-fixture-via-ui](../tasks/e2e-model-warning-fixture-via-ui.md) — drive explicit-model fixture through UI.
