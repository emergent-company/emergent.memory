## Why

The Google Drive data-source feature shipped (server `domain/sources` + `internal/gdrive`, gateway `datasources` pages, PR #1625) with server unit/integration tests and gateway render tests, but **no browser-level end-to-end coverage**. The connect → sync → version-history flow is exactly where a silent regression (a broken form contract, a dropped sync-job DTO field, a mis-rendered version state) ships undetected.

## What Changes

- Add a hermetic Playwright suite for the Data Sources (Google Drive) UI: list/empty state, schema-driven connect form + connection test, sync trigger/progress/cancel, and document version history.
- Extend the dev-mode mock harness (`mock-memory.mjs`) to serve the `data-source-integrations` and document-version endpoints, keyed so sync outcomes are deterministic (`folder-slow`/`folder-fail`/default).
- Add `datasources.config.ts` + a `task e2e:datasources` entry; exclude the specs from the live session-mode suite; make `run-e2e.sh` self-warm templ on an unwarmed tree.

No production behavior, API, or schema change.

## Capabilities

### New Capabilities

- `web-ui-e2e-datasources`: hermetic browser coverage of the Data Sources (Google Drive) connect, sync, and document-version UI flows.

### Modified Capabilities

<!-- None. Test-only addition; no spec'd production behavior changes. -->

## Impact

- `apps/web-ui/tests/e2e/**` — `mock-memory.mjs`, `datasources.config.ts`, `helpers/datasources.ts`, `specs/datasources/*`, `playwright.config.ts`, `README.md`, `run-e2e.sh`
- `apps/web-ui/Taskfile.yml` — `e2e:datasources`
- No production code, no API, no migration, no breaking change
