## 1. Restorer — per-column FK resolution policy

- [x] 1.1 Add `refAction` (remap/null/skip/fail) and `refPolicy` types; replace `restoreTableSpec.nullIfUnmapped` with `refs map[string]refPolicy`
- [x] 1.2 Convert every existing `nullIfUnmapped` column set to `{action: refNull}` verbatim (documents, graph_objects, graph_relationships, object_type_schemas, graph_schemas, product_versions, branches, agents, chat_conversations, object_extraction_jobs)
- [x] 1.3 Set `project_schemas.schema_id` and `project_edge_schema_registry.schema_id` to `{action: refSkip}`
- [x] 1.4 Add `applyRefs(row, remap, refs)` — rewrite in-remap values, apply null/skip/fail to non-empty unmapped values, default remap for absent columns
- [x] 1.5 Narrow `remapRowUUIDs` to PK registration; wire `applyRefs` into `insertTableRows` (clone only) with a warning log + `continue` on skip
- [x] 1.6 Pure unit tests for `applyRefs`: in-remap → remapped; refNull → nil; refSkip → skip; refFail → error; no policy → raw preserved

## 2. Migration — builtin graph schema uniqueness

- [x] 2.1 `00157_builtin_graph_schemas_unique.sql`: dedupe duplicate `source='builtin'` rows on `(name, version)` keeping one per group, then `CREATE UNIQUE INDEX ... WHERE source = 'builtin'`
- [x] 2.2 Scope to `source = 'builtin'` (not `project_id IS NULL`) so 00145's `source='manual'`, `project_id IS NULL` rows stay unconstrained
- [x] 2.3 Confirm the migration is picked up via `migrations/embed.go` (`//go:embed *.sql`) — no explicit list to update

## 3. DB-backed regression test

- [x] 3.1 Two test DBs as two deployments, each seeded with its own distinct builtin `graph_schemas` UUID
- [x] 3.2 Archive carries project-owned `graph_schemas` + both `project_schemas` links; clone insert against the target asserts: no FK error, owned link remapped, no reference to the source builtin, target's own builtin link present
- [x] 3.3 Overwrite (remap == nil) inserts unchanged
- [x] 3.4 Guard with `testing.Short()` and `t.Skipf` when the test DB is unavailable (matches existing `*_db_test.go` convention)

## 4. Verification

All Go commands run from `apps/server/`; `openspec` runs from the repository root.

- [x] 4.1 `go build ./...` clean
- [x] 4.2 `go test ./domain/backups/...` passes
- [x] 4.3 `go vet ./domain/backups/...` clean
- [x] 4.4 `golangci-lint run ./...` (or `task lint`) clean
- [x] 4.5 `openspec validate fix-clone-restore-global-schema-links` passes
