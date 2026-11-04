-- +goose Up
-- +goose NO TRANSACTION

-- Create ivfflat index for efficient cosine similarity search on relationship embeddings
-- 
-- Index Type: ivfflat (Inverted File with Flat Compression)
-- - Approximate nearest neighbor search (faster than exact)
-- - Trade-off: 95-99% recall with 10-100x speed improvement
-- - lists parameter: sqrt(row_count) is optimal for most cases
--   * lists=100 good for 10K rows (sqrt(10000) = 100)
--   * Scales to ~1M relationships before reindexing needed
-- 
-- Build Time: ~10 minutes per 1M rows on standard hardware
-- - For 100K rows: ~1 minute
-- - For 1M rows: ~10 minutes
-- - For 10M rows: ~100 minutes (consider partitioning)
-- 
-- Important: Using CONCURRENTLY to avoid table locks
-- - Allows reads/writes during index creation
-- - Takes longer but doesn't block production traffic
-- - Cannot run inside a transaction (hence NO TRANSACTION directive)

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_graph_relationships_embedding_ivfflat
ON kb.graph_relationships
USING ivfflat (embedding vector_cosine_ops)
WITH (lists = 100);

COMMENT ON INDEX kb.idx_graph_relationships_embedding_ivfflat IS 'IVFFlat index for cosine similarity search on relationship embeddings. Used by hybrid search to find semantically similar relationships. lists=100 optimized for up to ~1M relationships.';

-- Verification Query 1: Check index exists and is valid
-- SELECT 
--     schemaname, 
--     tablename, 
--     indexname, 
--     indexdef 
-- FROM pg_indexes 
-- WHERE schemaname = 'kb' 
--   AND tablename = 'graph_relationships' 
--   AND indexname = 'idx_graph_relationships_embedding_ivfflat';

-- Verification Query 2: Check index usage with EXPLAIN ANALYZE
-- EXPLAIN ANALYZE
-- SELECT id, src_id, dst_id, type, embedding <=> '[0.1,0.2,...]'::vector AS distance
-- FROM kb.graph_relationships
-- WHERE embedding IS NOT NULL
-- ORDER BY embedding <=> '[0.1,0.2,...]'::vector
-- LIMIT 10;
-- 
-- Expected output should include:
-- "Index Scan using idx_graph_relationships_embedding_ivfflat"

-- Verification Query 3: Check index statistics
-- SELECT 
--     schemaname, 
--     tablename, 
--     indexname, 
--     idx_scan, 
--     idx_tup_read, 
--     idx_tup_fetch 
-- FROM pg_stat_user_indexes 
-- WHERE schemaname = 'kb' 
--   AND tablename = 'graph_relationships' 
--   AND indexname = 'idx_graph_relationships_embedding_ivfflat';

-- +goose Down
-- +goose NO TRANSACTION

-- Drop the index (also uses CONCURRENTLY to avoid locks)
DROP INDEX CONCURRENTLY IF EXISTS kb.idx_graph_relationships_embedding_ivfflat;
