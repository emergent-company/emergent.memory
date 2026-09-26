-- +goose NO TRANSACTION
-- +goose Up
-- Indexes for admin/listing reads that otherwise full-scan kb.graph_objects
-- (dev: ~1.5 GB, one project = 94% of the table). Each was measured on dev
-- with a temporary index before landing.
--
-- All are built CONCURRENTLY (NO TRANSACTION) to avoid an access-exclusive lock
-- on a high-volume graph table. A failed CONCURRENTLY build leaves an INVALID
-- index behind, so drop any leftover first (same rationale as 00162/00163).

-- #1103: analytics/unused (GetUnused) orders by
-- (last_accessed_at ASC NULLS FIRST, id ASC) with "IS NULL OR < cutoff"; the
-- NULLS FIRST ordering had no index, so it was a parallel seq scan (~7.4 s).
-- Declaring the index order NULLS FIRST lets the planner satisfy the ordering
-- and stop at LIMIT.
DROP INDEX CONCURRENTLY IF EXISTS kb.idx_graph_objects_unused_main;
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_graph_objects_unused_main
  ON kb.graph_objects (project_id, last_accessed_at ASC NULLS FIRST, id ASC)
  WHERE supersedes_id IS NULL AND deleted_at IS NULL;

-- #1105: objects list filtered by status ordered by (created_at DESC, id DESC).
-- `status` is not indexed, so ?status=<value> seq-scanned the whole project
-- (~9.8 s) even when it matched nothing. Mirror the type index.
DROP INDEX CONCURRENTLY IF EXISTS kb.idx_graph_objects_head_status;
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_graph_objects_head_status
  ON kb.graph_objects (project_id, status, created_at DESC, id DESC)
  WHERE supersedes_id IS NULL AND branch_id IS NULL AND deleted_at IS NULL;

-- #1104: objects/tags does SELECT DISTINCT unnest(labels) over the project;
-- with no labels present this still seq-scanned the heap (~7.3 s). A partial
-- index over only labelled rows (paired with the cardinality(labels) > 0
-- predicate added to GetDistinctTags) makes it an index scan on the (empty)
-- labelled set.
DROP INDEX CONCURRENTLY IF EXISTS kb.idx_graph_objects_labels_head;
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_graph_objects_labels_head
  ON kb.graph_objects (project_id)
  WHERE supersedes_id IS NULL AND deleted_at IS NULL AND cardinality(labels) > 0;

-- Refresh planner stats so the new indexes are selected.
ANALYZE kb.graph_objects;

-- +goose Down
DROP INDEX IF EXISTS kb.idx_graph_objects_unused_main;
DROP INDEX IF EXISTS kb.idx_graph_objects_head_status;
DROP INDEX IF EXISTS kb.idx_graph_objects_labels_head;
