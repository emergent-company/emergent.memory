## 1. Migration

- [ ] 1.1 Add `apps/server/migrations/00170_embedding_indexes_hnsw.sql` with `-- +goose NO TRANSACTION`: for `kb.chunks.embedding` and `kb.skills.description_embedding`, `DROP INDEX CONCURRENTLY IF EXISTS <new>`, `CREATE INDEX CONCURRENTLY IF NOT EXISTS <new> ... USING hnsw (col vector_cosine_ops) WITH (m = 16, ef_construction = 64)`, then `DROP INDEX CONCURRENTLY IF EXISTS <old ivfflat>`, sequenced one index at a time. Include a `-- +goose Down` restoring the ivfflat indexes with `lists = 100`.
- [ ] 1.2 Verify the migration applies and reverts on a scratch database (never the shared dev DB): confirm the HNSW index exists with the right opclass/params, the ivfflat index is gone, and Down restores ivfflat.

## 2. Scheduler reindex targets

- [ ] 2.1 Remove `idx_chunks_embedding` and `idx_skills_embedding_ivfflat` from `embeddingIndexTargets` in `apps/server/domain/scheduler/embedding_index_reindex_task.go`; update the comment to explain that dropped/HNSW indexes must not be targeted.
- [ ] 2.2 Update `embedding_index_reindex_task_test.go` for the shrunken target list.

## 3. Align probes documentation and spec

- [ ] 3.1 Update the `ivfflat.probes` doc comments in `apps/server/domain/search/repository.go` and `apps/server/domain/skills/store.go` to state the setting is a harmless no-op for the HNSW chunk/skills indexes.
- [ ] 3.2 Update `openspec/specs/retrieval-performance-config/spec.md` via this change's delta so the spec matches the new index types.

## 4. Build, lint, verify

- [ ] 4.1 `cd apps/server && PATH="/root/go/bin:$PATH" go build ./...`
- [ ] 4.2 `cd apps/server && PATH="/root/go/bin:$PATH" go test ./domain/search/... ./domain/scheduler/...`
- [ ] 4.3 `cd apps/server && PATH="/root/go/bin:$PATH" golangci-lint run ./...`
- [ ] 4.4 `openspec validate embedding-indexes-hnsw` from the repo root.
