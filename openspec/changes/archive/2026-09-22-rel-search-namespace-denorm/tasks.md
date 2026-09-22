## 1. Migration

- [ ] 1.1 Add `apps/server/migrations/00172_graph_relationships_namespace.sql`: `ALTER TABLE kb.graph_relationships ADD COLUMN IF NOT EXISTS namespace TEXT`, partial btree index `idx_graph_relationships_namespace ON kb.graph_relationships (project_id, namespace) WHERE namespace IS NOT NULL`, and a backfill `UPDATE kb.graph_relationships gr SET namespace = src.namespace FROM kb.graph_objects src WHERE src.id = gr.src_id AND gr.namespace IS NULL`. Include a `-- +goose Down` that drops the index and column. Verify: apply + rollback on a scratch database.
- [ ] 1.2 Mirror the column + index in `apps/server/internal/testutil/schema.sql` so test scratch DBs match the model. Verify: `go test` DB suites can select/insert the new field.

## 2. Model and write paths

- [ ] 2.1 Add `Namespace *string` (`bun:"namespace"`) to `GraphRelationship` in `apps/server/domain/graph/entity.go`.
- [ ] 2.2 Inherit namespace across versions in `CreateRelationshipVersion` (`newVersion.Namespace = prevHead.Namespace`), covering tombstone/restore/patch/fast-forward/similarity-merge.
- [ ] 2.3 Set namespace on new HEAD inserts: `CreateRelationship` and `CreateSubgraph` from `srcObj.Namespace`; `maybeCreateInverse` from `dstObj.Namespace` (src/dst swapped); `applyMerge` clone from the source-branch head; `BulkCopyRelationshipsToBranch` from the source relationship.
- [ ] 2.4 Carry namespace through `BranchRelationshipHead` + `GetBranchRelationshipHeads`.

## 3. Query predicate + test

- [ ] 3.1 Extract `buildRelationshipSearchQuery` in `apps/server/domain/search/repository.go`; apply the namespace predicate on `r.namespace` when set, keep the nil/`all` path predicate-free.
- [ ] 3.2 Add `TestBuildRelationshipSearchQuery` asserting the predicate is `r.namespace = ?`, never `src.namespace`, and the arg ordering/length. Verify: `go test ./domain/search/...`.

## 4. Verification

- [ ] 4.1 `go build ./...` in `apps/server`.
- [ ] 4.2 `go test ./...` (at minimum `./domain/search/...`).
- [ ] 4.3 `golangci-lint run ./...` in `apps/server`.
- [ ] 4.4 Scratch-DB proof: migration Up/Down/Up and `EXPLAIN (ANALYZE, BUFFERS)` showing an index scan for the namespace-scoped query instead of a Parallel Seq Scan. Verify: plan output.
- [ ] 4.5 `openspec validate rel-search-namespace-denorm`.
