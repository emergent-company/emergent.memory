-- +goose Up
-- +goose StatementBegin
-- Add embedding_updated_at column to kb.graph_relationships
-- This was missed in migration 00011 which added the embedding column.
-- The Go model (GraphRelationship) already references this column, causing
-- INSERT failures: "column gr.embedding_updated_at does not exist"
ALTER TABLE kb.graph_relationships ADD COLUMN IF NOT EXISTS embedding_updated_at timestamp with time zone;

COMMENT ON COLUMN kb.graph_relationships.embedding_updated_at IS 'Timestamp when the embedding vector was last generated/updated. Used to track freshness of relationship embeddings.';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE kb.graph_relationships DROP COLUMN IF EXISTS embedding_updated_at;
-- +goose StatementEnd
