## 1. Repository — archive predicate + streaming iterators

- [x] 1.1 Add opt-in `ListParams.OnlyWithMigrationArchive` with a doc comment (batch-caller scan bound, relies on `idx_graph_objects_has_archive`)
- [x] 1.2 Apply `migration_archive <> '[]'::jsonb` in `buildObjectBaseQueryWith` when the flag is set
- [x] 1.3 Refactor `buildObjectBaseQuery` to delegate to `buildObjectBaseQueryWith(db, params)`; update `List`/`Count` usage without behaviour change
- [x] 1.4 Add `ListEach` / `ListEachTx` (reject `PropertyOrder`, default `desc`, page at `MaxListLimit`, call `fn(page)` per page, stop on `fn` error, no accumulation)
- [x] 1.5 Reimplement `ListAll` as a thin wrapper over `ListEach` (signature/behaviour preserved)
- [x] 1.6 DB tests: `OnlyWithMigrationArchive` filters empty-archive objects and pages; `ListEach` matches `ListAll` order; `ListEach` stops on `fn` error

## 2. Schemas service — bound migrate/rollback scans

- [x] 2.1 `RollbackSchemaMigration`: set `OnlyWithMigrationArchive` + `IncludeMigrationArchive`; move the scan inside `db.RunInTx` using `ListEachTx` (data + registry restore atomic in one transaction)
- [x] 2.2 `ExecuteSchemaMigration`: replace `ListAll` with `ListEach` per type (no archive predicate — must still visit archive-free objects); apply per-type `max_objects` cap during streaming with an early stop
- [x] 2.3 Add `SchemaMigrationRollbackRequest.MaxObjects` and apply it as a cap on restored objects; wire through the handler (JSON `max_objects,omitempty`)
- [x] 2.4 Add `Graph.MigrationScanMaxObjects` (`GRAPH_MIGRATION_SCAN_MAX_OBJECTS`, default `10000`) and enforce it in both execute and rollback (abort with a clear 4xx; rollback aborts inside the tx)
- [x] 2.5 Wire config into `schemas.NewService` via fx; update all `NewService` call sites
- [x] 2.6 DB tests: empty-archive execute still migrates/counts; rollback restores only archived objects; archive-free project restores zero; hard cap aborts; `MaxObjects` caps restores
- [x] 2.7 Reject `restore_type_registry` + `max_objects` combination up front (fail loudly with a 4xx before any work)
- [x] 2.8 Document abort semantics in the spec delta (rollback hard-cap abort atomic; execute non-transactional; combined-request rejection)
- [x] 2.9 Unit test asserting the combined request returns a 4xx and performs no work

## 3. Verification

- [x] 3.1 `go build ./...` clean
- [x] 3.2 `go vet ./...` clean
- [x] 3.3 `go test ./domain/graph/... ./domain/schemas/...` (unit tests pass; DB-dependent tests skip cleanly — Postgres unreachable in this environment)
- [x] 3.4 `task lint` clean
- [x] 3.5 `openspec validate bound-schema-migration-object-scan` passes
- [x] 3.6 Follow-up: reject-combination guard + spec/docs + ops note (this change)
