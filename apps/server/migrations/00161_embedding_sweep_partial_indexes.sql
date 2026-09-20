-- +goose Up
-- Partial indexes so the embedding sweep worker can find objects/relationships
-- with missing embeddings without a full table scan every sweep (30s cadence).
CREATE INDEX IF NOT EXISTS idx_graph_objects_missing_embedding
  ON kb.graph_objects (created_at)
  WHERE embedding_v2 IS NULL AND deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_graph_relationships_missing_embedding
  ON kb.graph_relationships (created_at)
  WHERE embedding IS NULL AND deleted_at IS NULL;

-- +goose Down
DROP INDEX IF EXISTS kb.idx_graph_objects_missing_embedding;
DROP INDEX IF EXISTS kb.idx_graph_relationships_missing_embedding;
