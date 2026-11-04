-- +goose NO TRANSACTION
-- +goose Up
-- The project-scoped embedding-progress endpoint (GET /api/embeddings/progress)
-- counts relationship embedding jobs per status for one project by joining
-- kb.graph_relationship_embedding_jobs j to kb.graph_relationships r on
-- r.id = j.relationship_id and grouping j.status. The relationship side is now
-- index-only (00191), but the jobs side still parallel-sequential-scans the
-- wide queue heap (avg_width ~480 B: the completed rows retain last_error text)
-- to fetch relationship_id + status. The existing btree on (relationship_id)
-- does not cover status, so the planner falls back to a full parallel seq scan.
-- This covering (relationship_id, status) index makes that side a Parallel
-- Index Only Scan (Heap Fetches: 0). The stale_failed count still uses the
-- status index and is unaffected.
--
-- Hermetic 82k-row bench (shared_buffers=16MB so the 25 MB jobs heap cannot
-- stay resident, matching dev's 16MB):
--   before: Parallel Seq Scan on graph_relationship_embedding_jobs, jobs read=3154
--   after:  Parallel Index Only Scan (relationship_id, status), jobs read=492
--
-- Built CONCURRENTLY (NO TRANSACTION) to avoid an access-exclusive lock on a
-- queue table; a failed CONCURRENTLY build leaves an INVALID index behind, so
-- drop any leftover first (same rationale as 00162/00163/00191).
DROP INDEX CONCURRENTLY IF EXISTS kb.idx_graph_rel_emb_jobs_relationship_status;

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_graph_rel_emb_jobs_relationship_status
  ON kb.graph_relationship_embedding_jobs (relationship_id, status);

-- Refresh stats after the index build so the new path is costed against
-- current data (the queue is written continuously by the embedding workers).
ANALYZE kb.graph_relationship_embedding_jobs;

-- +goose Down
DROP INDEX IF EXISTS kb.idx_graph_rel_emb_jobs_relationship_status;
