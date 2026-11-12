-- +goose NO TRANSACTION
-- +goose Up
-- Index behind embedding-cache retention (issue #684).
--
-- The cache_cleanup task now prunes kb.embedding_cache with
--
--   DELETE FROM kb.embedding_cache WHERE created_at < <cutoff>
--
-- kb.embedding_cache is a write-only cache: without this index the delete scans
-- the whole table every cleanup tick. The table can be large (one row per
-- embedded text, and old rows are orphaned rather than removed by a model switch
-- or a key-derivation change), so that scan is exactly the cost this index
-- removes.
--
-- Built CONCURRENTLY (NO TRANSACTION) like 00249/00294 so a large cache is not
-- locked exclusively while the index builds. A failed CONCURRENTLY build leaves
-- an INVALID index behind, so drop any leftover first.
DROP INDEX CONCURRENTLY IF EXISTS kb.idx_embedding_cache_created_at;
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_embedding_cache_created_at
  ON kb.embedding_cache (created_at);

-- +goose Down
-- Kept CONCURRENTLY and outside any StatementBegin/End block: goose runs a
-- StatementBegin block as a single ExecContext, which Postgres treats as a
-- transaction, where DROP INDEX CONCURRENTLY is illegal. As separate statements
-- under NO TRANSACTION each commits on its own.
DROP INDEX CONCURRENTLY IF EXISTS kb.idx_embedding_cache_created_at;
