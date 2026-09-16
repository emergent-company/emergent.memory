## 1. Repository — archive projection and full scans

- [x] 1.1 Add `IncludeMigrationArchive bool` to `ListParams` (opt-in; default off so the public list/search API is unchanged)
- [x] 1.2 Include `"migration_archive"` in the `List` projection only when `IncludeMigrationArchive` is set
- [x] 1.3 Add `Repository.ListAll` — cursor-paged (keyset `created_at,id`) full scan that is not capped at one page; reject `PropertyOrder` (incompatible with the cursor)
- [x] 1.4 Unit/DB test: `List` returns `MigrationArchive` when the flag is set and empty when it is not
- [x] 1.5 Unit/DB test: `ListAll` returns every object when the project holds more than one `MaxListLimit`-sized page

## 2. Schemas service — migrate and rollback paths

- [x] 2.1 Forward migrate path: set `IncludeMigrationArchive: true` so `MigrateObject` appends to the persisted slice instead of clobbering it
- [x] 2.2 Forward migrate path: list via `ListAll`; preserve `max_objects` by trimming the fetched set
- [x] 2.3 Rollback path: set `IncludeMigrationArchive: true` so objects are not skipped for an empty slice
- [x] 2.4 Rollback path: list via `ListAll` so objects beyond the first page are restored
- [x] 2.5 Leave the validate-objects and preview `List` call sites unchanged (they do not read or write `obj.MigrationArchive`)
- [x] 2.6 Service DB test: migrate-then-rollback restores the dropped property and consumes the archive entry
- [x] 2.7 Service DB test (regression): a second migration hop appends to the archive instead of destroying the first hop's entry
- [x] 2.8 Service DB test: migrate and rollback cover every object across multiple pages (`MaxListLimit` forced small)

## 3. Integration test

- [x] 3.1 HTTP end-to-end test in `apps/server/tests/integration/`: execute migration then rollback; assert `objects_restored == 1`, property restored, archive consumed
- [x] 3.2 Confirm the test fails against the pre-fix projection with `objects_restored: 0` (bug reproduction)

## 4. Verification

- [x] 4.1 `go build ./...` and `go vet ./...` clean
- [x] 4.2 `go test ./domain/graph/... ./domain/schemas/...` (new tests + existing suites pass; pre-existing 5436-dependent tests excluded)
- [x] 4.3 `golangci-lint run` shows no new issues in changed files
- [x] 4.4 Filtered integration test via `apps/server/Taskfile.yml` (`task test:integration -- -run TestBlueprintRollbackArchiveSuite`) passes
- [x] 4.5 `openspec validate fix-schema-migration-archive-projection` passes
