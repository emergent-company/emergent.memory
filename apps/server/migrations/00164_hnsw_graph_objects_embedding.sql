-- +goose NO TRANSACTION
-- +goose Up
-- Replace the IVFFlat index on kb.graph_objects.embedding_v2 with HNSW.
--
-- The similar-objects query (GET /api/graph/objects/{id}/similar) orders by
-- `embedding_v2 <=> $vec` and the app raises ivfflat.probes to 10
-- (SEARCH_IVFFLAT_PROBES) for recall. On a ~107k-row / 768-dim project each
-- probe reads one more ~4MB list and the relational filters (project_id,
-- supersedes_id IS NULL, branch_id IS NULL, deleted_at IS NULL) cannot fold
-- into the approximate distance ordering, so probes=10 makes the scan ~3x
-- slower than probes=1 (683ms warm, ~3.5s cold) with ~10x the I/O. Lowering
-- probes instead collapses recall (<1% of rows), and the similar panel drops
-- <=80% matches, so it renders empty. HNSW needs no training step or probes
-- tuning and gives near-exact recall in sub-ms at this size — matching the
-- guidance already in the search domain (search/repository.go): prefer an HNSW
-- index over forcing ivfflat.probes planner settings.
--
-- Built CONCURRENTLY (NO TRANSACTION) to avoid an access-exclusive lock on a
-- high-volume graph table. A failed CONCURRENTLY build leaves an INVALID index
-- behind, so drop any leftover first so a retry rebuilds from scratch (same
-- rationale as 00162/00163). Requires pgvector >= 0.5.0 for HNSW support.
DROP INDEX CONCURRENTLY IF EXISTS "IDX_graph_objects_embedding_v2_hnsw";

CREATE INDEX CONCURRENTLY IF NOT EXISTS "IDX_graph_objects_embedding_v2_hnsw"
    ON kb.graph_objects USING hnsw (embedding_v2 vector_cosine_ops)
    WITH (m = 16, ef_construction = 64);

-- Drop the now-superseded IVFFlat index once HNSW is in place. Two ~420MB ANN
-- indexes on one column doubles storage and gives the planner a second
-- approximate path to choose poorly.
DROP INDEX CONCURRENTLY IF EXISTS "IDX_graph_objects_embedding_v2_ivfflat";

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS "IDX_graph_objects_embedding_v2_hnsw";

CREATE INDEX CONCURRENTLY IF NOT EXISTS "IDX_graph_objects_embedding_v2_ivfflat"
    ON kb.graph_objects USING ivfflat (embedding_v2 vector_cosine_ops)
    WITH (lists = 100);
