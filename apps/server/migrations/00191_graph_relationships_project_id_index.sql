-- +goose NO TRANSACTION
-- +goose Up
-- The project-scoped embedding-progress endpoint (GET /api/embeddings/progress)
-- counts relationship embedding jobs per status for one project. That query
-- joins kb.graph_relationship_embedding_jobs to kb.graph_relationships and
-- filters on r.project_id while reading r.id for the join. Neither
-- idx_graph_relationships_head_main (project_id, created_at DESC, id DESC) nor
-- the PK is chosen for it: the planner seq-scans the whole graph_relationships
-- heap. This narrower (project_id, id) index is selected and turns the
-- relationship side into an index-only scan.
--
-- Dev EXPLAIN (project Norwegian Law, ~82k HEAD relationships):
--   before: Parallel Seq Scan on graph_relationships (~1239 ms of the leg),
--           relationship StatsByProject leg ~3529 ms
--   after:  Parallel Index Only Scan using (project_id, id) (~8 ms),
--           leg ~2239 ms (remaining cost is the queue-table heap scan)
--
-- Built CONCURRENTLY (NO TRANSACTION) to avoid an access-exclusive lock on a
-- high-volume graph table. A failed CONCURRENTLY build leaves an INVALID index
-- behind, so drop any leftover first (same rationale as 00162/00163).
DROP INDEX CONCURRENTLY IF EXISTS kb.idx_graph_relationships_project_id_id;

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_graph_relationships_project_id_id
  ON kb.graph_relationships (project_id, id);

-- kb.graph_relationship_embedding_jobs had never been analyzed (last_analyze
-- NULL on dev), so the progress aggregates planned from default estimates.
-- Refresh planner stats for the relationship table and both embedding queues.
ANALYZE kb.graph_relationships;
ANALYZE kb.graph_embedding_jobs;
ANALYZE kb.graph_relationship_embedding_jobs;

-- +goose Down
DROP INDEX IF EXISTS kb.idx_graph_relationships_project_id_id;
