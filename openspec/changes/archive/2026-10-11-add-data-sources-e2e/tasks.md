## 1. Harness

- [x] 1.1 Extend `mock-memory.mjs` with the data-source-integrations state + routes (providers, provider schema, source-types, config test, integration CRUD, sync trigger, sync-jobs list/latest/detail/cancel, connection test) and document + version-history reads.
- [x] 1.2 Key sync outcomes off `folder_id` (`folder-slow` → processing, `folder-fail` → failed, default → completed); keep credentials write-only (never echoed).
- [x] 1.3 Seed multi-version documents (`doc-report` two active versions; `doc-removed` with a `deleted_from_source` latest version).
- [x] 1.4 Add `datasources.config.ts` (hermetic: `webServer` runs `run-e2e.sh`, serial workers, baseURL `http://localhost:8097`).
- [x] 1.5 Make `run-e2e.sh` generate templ when `*_templ.go` is absent (unwarmed tree).

## 2. Specs

- [x] 2.1 `specs/datasources/data-sources-list.spec.ts` — empty state + call to action, populated row with provider/status/last-sync, row → detail.
- [x] 2.2 `specs/datasources/data-sources-connect.spec.ts` — schema-driven form render, config test success/failure, required-field rejection, create → detail.
- [x] 2.3 `specs/datasources/data-sources-sync.spec.ts` — trigger + progress + cancel, failed sync, completed counts + last-synced, existing connection test.
- [x] 2.4 `specs/datasources/document-versions.spec.ts` — multi-version history, `deleted_from_source` state.
- [x] 2.5 `helpers/datasources.ts` — `createDriveSource` / `deleteDriveSource` (self-cleanup).

## 3. Wiring

- [x] 3.1 Exclude `specs/datasources/**` from the live `chromium` project (`playwright.config.ts` `testIgnore`).
- [x] 3.2 Add `task e2e:datasources` to `apps/web-ui/Taskfile.yml`.
- [x] 3.3 Document the suite in `apps/web-ui/tests/e2e/README.md`.

## 4. Verification

- [x] 4.1 Gateway builds and `npm`/Playwright deps resolve; `npx playwright test --config=datasources.config.ts` passes.
- [x] 4.2 `node --check mock-memory.mjs` and TypeScript compiles (Playwright list).
- [x] 4.3 No spec leaves a source behind (serial + self-clean); re-run is green.
