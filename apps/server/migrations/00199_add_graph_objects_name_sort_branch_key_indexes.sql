-- +goose NO TRANSACTION
-- +goose Up
-- Two partial head-main indexes for entity-query (issue #1206).
--
-- 1) sort_by=name ordering. entity-query emits
--    ORDER BY left(go.properties->>'name', 256) DESC NULLS LAST, go.key, and no
--    index covered it. A sort_by=name call with no narrowing key_prefix
--    therefore seq/bitmap-scanned every HEAD row of the type, detoasted the
--    wide `properties` JSONB and sorted before LIMIT. On a dev-shaped seeded
--    corpus (19.2k LegalParagraph rows, ~14 KB content/row) the whole-type sort
--    ran ~274 ms with a Sort node; this expression index turns the same query
--    into an ordered Index Scan with no Sort (~0.34 ms).
--
--    The expression and its 256-char bound are load-bearing: `properties` is
--    user-supplied with no size cap (domain/graph/validation.go passes unknown
--    property types through), so indexing the raw properties->>'name' makes any
--    oversized name fail the insert with SQLSTATE 54000 ("index row size ...
--    exceeds btree version 4 maximum"), and would also make CREATE INDEX
--    CONCURRENTLY itself fail on a pre-existing oversized row. 256 chars x 4
--    bytes/UTF-8 rune = 1024 B, plus the bounded key (<=128 runes -> <=512 B)
--    and type (<=64 runes -> <=256 B) columns, stays well under the ~2704 B
--    btree limit; 512 chars (2048 B for the expression alone) could exceed it.
--    entity-query's ORDER BY uses the exact same left(..., 256) expression and a
--    go.key tiebreak, so the index is still used with no Sort and the order is
--    total/deterministic. Names sharing the first 256 chars therefore tie and
--    are then ordered by the canonical key. Mirrors the created_at ordering
--    pattern of 00162 / 00163.
--
-- 2) branch-scoped key_prefix. idx_graph_objects_project_type_key_c (00198) is
--    partial on branch_id IS NULL, and IDX_graph_objects_upsert_branch uses the
--    database default collation, so a branch-scoped key_prefix range
--    (key COLLATE "C") cannot be an index condition and falls back to a scan of
--    the branch's rows of the type. The COLLATE "C" branch index makes it an
--    Index Scan (~7.9 ms -> 0.19 ms on a 19.2k-row branch); partial on
--    branch_id IS NOT NULL keeps it to branch rows only, since entity-query
--    always pins a concrete branch_id on that path.
--
-- Both indexes are partial on the head-main predicate the query uses and are
-- built CONCURRENTLY (NO TRANSACTION) to avoid an access-exclusive lock on the
-- high-volume graph table; a failed CONCURRENTLY build leaves an INVALID index
-- behind, so drop any leftover first (see 00162 for the same rationale).

DROP INDEX CONCURRENTLY IF EXISTS kb.idx_graph_objects_project_type_name;
DROP INDEX CONCURRENTLY IF EXISTS kb.idx_graph_objects_project_branch_type_key_c;

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_graph_objects_project_type_name
  ON kb.graph_objects (project_id, type, left(properties->>'name', 256) DESC NULLS LAST, key)
  WHERE deleted_at IS NULL AND supersedes_id IS NULL AND branch_id IS NULL;

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_graph_objects_project_branch_type_key_c
  ON kb.graph_objects (project_id, branch_id, type, key COLLATE "C")
  WHERE deleted_at IS NULL AND supersedes_id IS NULL AND branch_id IS NOT NULL;

-- Refresh planner stats so both indexes are selected on their paths.
ANALYZE kb.graph_objects;

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS kb.idx_graph_objects_project_branch_type_key_c;
DROP INDEX CONCURRENTLY IF EXISTS kb.idx_graph_objects_project_type_name;
