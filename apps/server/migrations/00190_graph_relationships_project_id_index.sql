-- +goose NO TRANSACTION
-- +goose Up
-- The project-scoped embedding-progress endpoint (GET /api/embeddings/progress)
-- counts relationship embedding jobs per status for one project. That query
-- joins kb.graph_relationship_embedding_jobs to kb.graph_relationships and
-- filters on r.project_id while reading r.id for the join. With no
-- project-leading index the planner seq-scans the whole graph_relationships
-- heap (measured ~12k buffers read, ~2.6s on a large project); this index turns
-- the relationship side into an index-only scan (measured ~0.04-0.07s).
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
