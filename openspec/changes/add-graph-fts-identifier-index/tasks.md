## 1. Migration — rebuild the graph-object FTS index (issue #706)

- [x] 1.1 Add `apps/server/migrations/00174_graph_objects_fts_identifiers.sql`: immutable helper `kb.graph_object_fts(key, type, properties)` indexing raw + separator-normalised + signed-normalised key (weight A), type (B), bounded `title`/`name`/`description` under `norwegian` (C). Verify: goose applies it on a scratch database.
- [x] 1.2 Redefine `kb.update_graph_objects_fts()` to delegate to the helper, backfill existing rows (`WHERE fts IS DISTINCT FROM ...`, idempotent), drop and rebuild `kb.idx_graph_objects_fts` CONCURRENTLY under `-- +goose NO TRANSACTION`. Verify: `goose up` completes; `pg_indexes` shows the index; the table has no NULL/old-shape vectors.
- [x] 1.3 Write the Down migration restoring the exact 00016 function and expression, re-backfilling, rebuilding the index, and dropping the helper. Verify: `goose down` restores the old function and drops the helper.
- [x] 1.4 Document lock impact and rollback in the migration header. Verify: header present.

## 2. Query side — dual-configuration match and rank

- [x] 2.1 Update `graph.Repository.ftsSearch` to match `fts @@ websearch_to_tsquery('simple', ?) OR fts @@ websearch_to_tsquery('norwegian', ?)` and to rank with `GREATEST(ts_rank_cd(simple), ts_rank_cd(norwegian))`, keeping placeholder order correct. Verify: `go build ./...`.
- [x] 2.2 Note in `FTSSearch` that the #704 `ftsquery.Relax` fallback is retained as a safety net and is no longer the path that makes composite keys work. Verify: comment updated.

## 3. Tests

- [x] 3.1 Add `apps/server/domain/graph/fts_identifier_index_db_test.go`: applies the migration's function definitions to the throwaway test DB, then asserts the strict dual query (no fallback) matches `aksjeloven lov 1997-06-13-44`, that component/raw/stemmed queries all return the law, and that a 40000-token description produces no position at MAXENTRYPOS. Verify: `go test ./domain/graph/ -run TestFTSIdentifierIndex -count=1`.
- [x] 3.2 Confirm the #704 relax tests still pass. Verify: `go test ./domain/graph/ -run 'TestFTSSearch' -count=1`.

## 4. Verification

- [x] 4.1 `go build ./...`, `go test ./domain/graph/... ./domain/search/... -count=1`, `golangci-lint run ./...` in `apps/server`. Verify: no new failures or lint issues attributable to this change.
- [x] 4.2 Prove the migration forward/backward on a scratch database with goose, and capture before/after recall and `EXPLAIN` evidence. Verify: recorded in the PR.
