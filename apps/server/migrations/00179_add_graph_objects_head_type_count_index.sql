-- +goose NO TRANSACTION
-- +goose Up
-- Partial covering index for the MCP entity-type-list count query
-- (search-knowledge -> /query -> graph-query-agent -> entity-type-list).
-- The entity-types query counts kb.graph_objects per (project_id, type) with the
-- predicates deleted_at IS NULL / supersedes_id IS NULL / branch_id IS NULL, but
-- the only usable index was idx_graph_objects_project_type_created_id (00162),
-- whose (project_id, type, created_at, id) columns force reading every matching
-- row and then filtering deleted/supersedes/branch in the heap, producing a
-- 101k-row nested loop plus external disk sort (~105s) that exceeds the 60s
-- search-knowledge timeout (issue #915).
-- This partial index matches the predicate exactly and carries `id` so
-- COUNT(go.id) is satisfied by an index-only scan.
-- Built CONCURRENTLY (NO TRANSACTION) to avoid an access-exclusive lock on the
-- high-volume graph table. Drop any leftover first so a retry rebuilds it from
-- scratch (see 00162 for the same rationale).
DROP INDEX CONCURRENTLY IF EXISTS kb.idx_graph_objects_head_type_count;

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_graph_objects_head_type_count
  ON kb.graph_objects (project_id, type, id)
  WHERE deleted_at IS NULL AND supersedes_id IS NULL AND branch_id IS NULL;

-- Refresh planner stats so the new index is selected for the count query.
ANALYZE kb.graph_objects;

-- +goose Down
DROP INDEX IF EXISTS kb.idx_graph_objects_head_type_count;
