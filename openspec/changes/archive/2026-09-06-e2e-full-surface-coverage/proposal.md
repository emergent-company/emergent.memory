## Why

The Playwright suite (archived change `add-playwright-e2e-suite`) covers the tenant lifecycle and 13 specs, but most of the gateway's UI surface is untested — agent detail/settings/sandbox, documents detail, objects, schema, blueprints, skills, schedules, backups, sessions, usage, most of Settings, org tool-settings, invites, and account tokens. Selectors today lean on roles/text (brittle); stable `data-testid` anchors would make navigation deterministic. We want 100% page coverage plus a test-ID infrastructure to keep the suite robust as the UI grows.

## What Changes

- Add `data-testid` anchors to stable UI elements across the shell and pages (via go-daisy component `Attrs` and raw HTML in `.templ`), following a naming convention.
- Expand the e2e suite to cover the remaining UI surface: smoke-render specs for every route, plus UI-driven mutation specs for the main entities.
- No product behavior change — test infrastructure + test-only DOM attributes only (`skip_specs: true`).

## Capabilities

### New Capabilities

None — no spec-level behavior change.

### Modified Capabilities

None.

## Impact

- `gateway/*.templ` — add `data-testid` attributes (test hooks, invisible to users).
- `tests/e2e/specs/*` — new smoke + mutation specs; extend helpers for seeded/created fixtures.
- `tests/e2e/README.md` — document the test-ID convention and the expanded run instructions.
- No changes to API contracts, backend behavior, or the memory backend.
