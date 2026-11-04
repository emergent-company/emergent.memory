-- +goose NO TRANSACTION
-- +goose Up
-- Partial indexes so GET /api/embeddings/coverage can count embedded
-- (vector-present) live graph rows per project without a full table scan. The
-- awaiting side (vector-missing) is already served by
-- idx_graph_objects_missing_embedding / idx_graph_relationships_missing_embedding
-- (00161); these add the complementary "has a vector" predicate on the
-- project_id key.
--
-- Built CONCURRENTLY (NO TRANSACTION) so the migration does not take an
-- access-exclusive lock while scanning these high-volume graph tables. A failed
-- CONCURRENTLY build leaves an INVALID index behind, so drop any leftover first
-- (same rationale as 00161/00192).
DROP INDEX CONCURRENTLY IF EXISTS kb.idx_graph_objects_embedding_coverage;
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_graph_objects_embedding_coverage
  ON kb.graph_objects (project_id)
  WHERE embedding_v2 IS NOT NULL AND deleted_at IS NULL;

DROP INDEX CONCURRENTLY IF EXISTS kb.idx_graph_relationships_embedding_coverage;
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_graph_relationships_embedding_coverage
  ON kb.graph_relationships (project_id)
  WHERE embedding IS NOT NULL AND deleted_at IS NULL;

-- +goose Down
DROP INDEX IF EXISTS kb.idx_graph_objects_embedding_coverage;
DROP INDEX IF EXISTS kb.idx_graph_relationships_embedding_coverage;
