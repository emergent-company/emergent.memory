-- +goose NO TRANSACTION
-- +goose Up
-- Replace the IVFFlat index on kb.graph_relationships.embedding with HNSW.
--
-- The relationship ANN leg of unified search orders by `embedding <=> $vec` and
-- the app raised ivfflat.probes to improve recall. On a project with ~82k
-- embedded 768-dim relationships the index was measured at 336MB and pgvector's
-- ivfflat cost estimate scales roughly linearly with `ivfflat.probes`, while the
-- competing Parallel Seq Scan estimate prices the TOASTed vectors as free to
-- read. The estimates cross over between probes=5 and probes=10, so at the
-- shared default (SEARCH_IVFFLAT_PROBES=10) the planner abandoned the index:
--
--   probes=10 -> Parallel Seq Scan + Sort   400,072 ms (src.project_id)
--   probes=10 -> Parallel Seq Scan + Sort    86,674 ms (r.project_id)
--   probes=5  -> Index Scan (cost ~5.2k)        248 ms
--   probes=1  -> Index Scan (cost ~1.0k)        451 ms
--
-- PR #696 pinched this to a relationship-only probes=5, which is a stopgap: it
-- traded recall (~90% -> ~75%) for a plan crossover that moves as data grows.
-- HNSW has no probes knob, needs neither training nor list tuning, and keeps
-- near-exact recall at comparable latency, so this entire class of plan flip
-- disappears and `SET LOCAL ivfflat.probes` becomes a no-op for this index.
--
-- Built CONCURRENTLY (NO TRANSACTION) to avoid an access-exclusive lock on a
-- high-volume graph table, and sequenced create-new-before-drop-old so an ANN
-- index is always available. A failed CONCURRENTLY build leaves an INVALID index
-- behind, so drop any leftover first so a retry rebuilds from scratch (same
-- rationale as 00164/00162/00163). Requires pgvector >= 0.5.0 for HNSW support.
DROP INDEX CONCURRENTLY IF EXISTS kb.idx_graph_relationships_embedding_hnsw;

-- The index name must stay unqualified: CREATE INDEX derives the schema from
-- the (already schema-qualified) table reference and rejects a qualified name.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_graph_relationships_embedding_hnsw
    ON kb.graph_relationships USING hnsw (embedding vector_cosine_ops)
    WITH (m = 16, ef_construction = 64);

-- Drop the now-superseded IVFFlat index only after HNSW exists. Two large ANN
-- indexes on one column doubles storage and gives the planner a second
-- approximate path to choose poorly.
DROP INDEX CONCURRENTLY IF EXISTS kb.idx_graph_relationships_embedding_ivfflat;

-- +goose Down
-- Recreate the previous IVFFlat index and drop HNSW. Same create-before-drop
-- ordering keeps an ANN index available throughout the rollback.
DROP INDEX CONCURRENTLY IF EXISTS kb.idx_graph_relationships_embedding_hnsw;

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_graph_relationships_embedding_ivfflat
    ON kb.graph_relationships USING ivfflat (embedding vector_cosine_ops)
    WITH (lists = 100);
