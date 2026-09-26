-- +goose NO TRANSACTION
-- +goose Up
-- Partial index for the relationships list query
-- (GET /api/graph/relationships/search, no type/src/dst filter). That query
-- filters by project_id + supersedes_id IS NULL + branch_id IS NULL +
-- deleted_at IS NULL and orders by (created_at ASC, id ASC) — see
-- domain/graph/repository.go ListRelationships. No existing index matches it:
-- graph_relationships only had idx_graph_relationships_missing_embedding
-- (created_at) WHERE embedding IS NULL, so the list fell back to a parallel
-- seq scan + top-N sort over every relationship in the project.
--
-- Measured on dev (project with ~82k HEAD/main relationships):
--   Parallel Seq Scan on graph_relationships (8594 buffers) -> Sort -> Limit
--   Execution Time: 1222 ms   (API: relationships/search 3.1-4.6 s)
-- Mirrors idx_graph_objects_head_main (00163) for kb.graph_objects, which is
-- why the objects list is fast. The index serves the (created_at, id)
-- ordering in both directions (forward for ASC, backward scan for DESC), so
-- the default ?order=asc and ?order=desc pagination both use it.
--
-- Built CONCURRENTLY (NO TRANSACTION) to avoid an access-exclusive lock on a
-- high-volume graph table. A failed CONCURRENTLY build leaves an INVALID index
-- behind, so drop any leftover first (same rationale as 00162/00163).
DROP INDEX CONCURRENTLY IF EXISTS kb.idx_graph_relationships_head_main;

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_graph_relationships_head_main
  ON kb.graph_relationships (project_id, created_at, id)
  WHERE supersedes_id IS NULL AND branch_id IS NULL AND deleted_at IS NULL;

-- Refresh planner stats so the new index is selected instead of the seq scan.
ANALYZE kb.graph_relationships;

-- +goose Down
DROP INDEX IF EXISTS kb.idx_graph_relationships_head_main;
