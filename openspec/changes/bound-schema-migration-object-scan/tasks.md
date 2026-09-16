## 1. Repository — archive predicate on the object scan

- [ ] 1.1 Add an opt-in `ListParams` flag (e.g. `OnlyWithMigrationArchive`) to bound the scan to archive-carrying objects
- [ ] 1.2 Apply the `migration_archive <> '[]'::jsonb` predicate in the object base query when the flag is set (both `List` and `ListAll`)
- [ ] 1.3 DB test: `ListAll` with the flag returns only objects whose `migration_archive` is non-empty; without the flag it returns every object

## 2. Schemas service — bound migrate/rollback scans

- [ ] 2.1 `RollbackSchemaMigration`: set the archive predicate on its `ListAll` call
- [ ] 2.2 `ExecuteSchemaMigration`: set the archive predicate on its `ListAll` call (only where it reads/writes archives)
- [ ] 2.3 Service DB test: rollback over a project whose objects carry no archive scans only archive-carrying objects and still restores the matching ones

## 3. Verification

- [ ] 3.1 `go build ./...` clean
- [ ] 3.2 `go vet ./...` clean
- [ ] 3.3 `go test ./domain/graph/... ./domain/schemas/...` passes (new + existing; pre-existing DB-dependent tests excluded)
- [ ] 3.4 `openspec validate bound-schema-migration-object-scan` passes
