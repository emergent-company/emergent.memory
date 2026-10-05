## 1. Chokepoint guard

- [x] 1.1 Add an optional `GenerativeModelResolver` seam (`WithGenerativeModelResolver`) and the `ErrNoGenerativeModel` sentinel to `ObjectExtractionJobsService`.
- [x] 1.2 Gate `CreateJob` (the single creation chokepoint) on the resolved effective generative model; empty model => sentinel; nil resolver or resolution error => fail open. Verify `go build ./...`.
- [x] 1.3 Attach `*modelconfig.Service` to the shared jobs service via fx (`provideObjectExtractionJobsService`), covering admin API, MCP `queue-reextraction`, and document auto-extract.
- [x] 1.4 Map the sentinel to `409 no_generative_model` in `AdminHandler.CreateJob`; remove the now-redundant handler-level guard. Verify membership is still enforced before the gate.

## 2. Caller behavior

- [x] 2.1 MCP `queue-reextraction` returns a clear tool error (not a panic/500) when the gate rejects.
- [x] 2.2 Document auto-extract (raw-content create / post-parse) logs and skips the job; the document upload/parse still succeeds (no regression of #1522).
- [x] 2.3 Retry / bulk-retry (rows already exist) left unchanged.

## 3. Tests

- [x] 3.1 Chokepoint: reject with `ErrNoGenerativeModel` + assert 0 rows; create with model + assert 1 row.
- [x] 3.2 Admin HTTP: `409 no_generative_model` + 0 rows; `201` with model; fail-open on resolver error.
- [x] 3.3 MCP: reject path returns a clear error + 0 rows; success path creates a job.
- [x] 3.4 Auto-extract: `TriggerForDocument` rejects + 0 rows without a model; creates with a model.
- [x] 3.5 Document upload with `autoExtract=true` returns `201` and inserts 0 rows when no model resolves; `1` row with a model.
- [x] 3.6 Mutation-test the chokepoint gate (no resolver / inverted empty-model condition fails the reject tests) and the auto-extract skip; restore.
- [x] 3.7 Run `TEST_DATABASE_URL=... REQUIRE_DB=1 go test -count=1 ./domain/extraction/... ./domain/modelconfig/... ./domain/mcp/... ./domain/documents/...`.
- [x] 3.8 API e2e: seed a project generative model in the extraction list/cancel/statistics tests so job creation succeeds under the new gate (`ensureGenerativeModelConfigured`); API Tests green.

## 4. Verification & delivery

- [x] 4.1 `go build ./...` from `apps/server`.
- [x] 4.2 `bash apps/server/scripts/lint-ratchet.sh` (no ratchet growth).
- [x] 4.3 `bash scripts/preflight/migration-number.sh` (no migration added).
- [x] 4.4 `openspec validate extraction-job-model-guard --strict`.
- [x] 4.5 Commit, push `fix/extraction-job-model-guard`, update PR referencing `Fixes #1555`.
