-- +goose Up
-- +goose StatementBegin
-- Add embedding column to kb.graph_relationships for triplet-based semantic search
-- Column is nullable to support gradual rollout:
-- - New relationships will have embeddings generated synchronously
-- - Existing relationships can be backfilled via batch script
-- - Search queries filter WHERE embedding IS NOT NULL
ALTER TABLE kb.graph_relationships ADD COLUMN embedding vector(768);

COMMENT ON COLUMN kb.graph_relationships.embedding IS 'Vector embedding of relationship triplet text (e.g., "Elon Musk founded Tesla"). Generated from source.name + relation_type + target.name using Vertex AI text-embedding-004. Nullable to support backfill of existing relationships.';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE kb.graph_relationships DROP COLUMN IF EXISTS embedding;
-- +goose StatementEnd
