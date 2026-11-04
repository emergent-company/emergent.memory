-- +goose NO TRANSACTION
-- +goose Up
-- Migration 00193 added the covering composite
--   idx_graph_rel_emb_jobs_relationship_status (relationship_id, status)
-- on kb.graph_relationship_embedding_jobs. The older single-column
--   idx_graph_rel_emb_jobs_relationship_id (relationship_id)
-- (created in 00033) is now a strict prefix of that composite, so it can never
-- be selected for a plan the composite does not already serve — the leading
-- relationship_id column covers every lookup and FK cascade the old index
-- supported. It is redundant: this is a write-heavy queue table, so keeping it
-- only adds insert/update and disk cost.
--
-- DROP INDEX CONCURRENTLY (NO TRANSACTION) avoids an access-exclusive lock on
-- the queue; IF EXISTS keeps the migration idempotent on databases where a
-- concurrent DDL was already interrupted. The DOWN recreates the 00033 index.
DROP INDEX CONCURRENTLY IF EXISTS kb.idx_graph_rel_emb_jobs_relationship_id;

-- +goose Down
CREATE INDEX IF NOT EXISTS idx_graph_rel_emb_jobs_relationship_id
  ON kb.graph_relationship_embedding_jobs (relationship_id);
