-- +goose NO TRANSACTION
-- +goose Up
-- Add partial unique indexes to prevent duplicate active embedding jobs.
-- These work in tandem with the application-level SELECT-before-INSERT dedup
-- to eliminate the race window between the check and the insert.
-- ON CONFLICT DO NOTHING is used in the batch insert paths so duplicate
-- enqueue attempts are silently ignored rather than erroring.

CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uidx_graph_embedding_jobs_active
    ON kb.graph_embedding_jobs (object_id)
    WHERE status IN ('pending', 'processing');

CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uidx_graph_rel_embedding_jobs_active
    ON kb.graph_relationship_embedding_jobs (relationship_id)
    WHERE status IN ('pending', 'processing');

CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uidx_chunk_embedding_jobs_active
    ON kb.chunk_embedding_jobs (chunk_id)
    WHERE status IN ('pending', 'processing');

-- +goose Down
DROP INDEX IF EXISTS kb.uidx_graph_embedding_jobs_active;
DROP INDEX IF EXISTS kb.uidx_graph_rel_embedding_jobs_active;
DROP INDEX IF EXISTS kb.uidx_chunk_embedding_jobs_active;
