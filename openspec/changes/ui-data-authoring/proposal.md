## Why

The e2e suite can now cover object/schema/blueprint detail by seeding a bundled schema pack (already done), but object types are still **only** creatable by installing a bundled or registry pack — there is no way to author a schema (object type or blueprint) directly in the UI. That blocks "create data via UI" both for tests and for real users who want a custom graph without a prebuilt pack. We want UI-first schema authoring, plus a deterministic LLM mode to eventually unblock the deep/flaky tier (chat stream, sandbox, extraction).

## What Changes

- **B.1 — Object-type builder**: define a custom object type (name + typed properties) from the UI; it becomes a project-scoped schema and is selectable in object-create.
- **B.2 — Blueprint draft author**: create a private blueprint draft (name + object types) from the UI and install it, reusing the existing install/apply path.
- **B.3 — Deterministic test LLM** (cross-repo, memory backend): a project flag that makes generate calls return a canned response, so chat/sandbox/extraction/session/run can be e2e-tested deterministically.

## Capabilities

### New Capabilities
- `schema-authoring`: author object types and blueprint drafts via the UI.

### Modified Capabilities

None.

## Impact

- `gateway/` — schema page (object-type builder), blueprints gallery (draft author), new `POST /api/schema/object-types` + draft-create endpoints.
- `emergent.memory` (separate repo) — B.3 test-LLM mode; tracked as a cross-repo task/issue, not a spec in this repo.
- `tests/e2e/` — specs that seed a minimal type via the UI instead of relying on the bundled `personal-memory` pack.
