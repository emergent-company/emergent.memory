-- +goose NO TRANSACTION
-- +goose Up
-- Partial index for the unfiltered objects list query
-- (GET /api/graph/objects/search?limit=100, no type/branch/ids filter). That
-- query filters by project_id + supersedes_id IS NULL + branch_id IS NULL +
-- deleted_at IS NULL and orders by (created_at DESC, id DESC). No existing
-- index matches it: idx_graph_objects_project_type_created_id (00162) has
-- `type` mid-index and only serves the ?type= filtered case, so the unfiltered
-- list fell back to a parallel seq scan over the whole 708MB table (~13s).
-- This partial index matches the predicate exactly and serves the ordering, so
-- the planner uses an index scan and stops at LIMIT.
-- Built CONCURRENTLY (NO TRANSACTION) to avoid an access-exclusive lock on a
-- high-volume graph table. A failed CONCURRENTLY build leaves an INVALID index
-- behind, so drop any leftover first (see 00162 for the same rationale).
DROP INDEX CONCURRENTLY IF EXISTS kb.idx_graph_objects_head_main;

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_graph_objects_head_main
  ON kb.graph_objects (project_id, created_at DESC, id DESC)
  WHERE supersedes_id IS NULL AND branch_id IS NULL AND deleted_at IS NULL;

-- Refresh planner stats so the new index is selected and the multi-hundred-ms
-- planning time on the objects search / similar paths does not regress.
ANALYZE kb.graph_objects;

-- +goose Down
DROP INDEX IF EXISTS kb.idx_graph_objects_head_main;
