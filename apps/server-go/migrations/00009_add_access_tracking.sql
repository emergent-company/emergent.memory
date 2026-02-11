-- +goose Up
-- +goose StatementBegin
-- Add last_accessed_at column to track when graph objects are accessed via search
ALTER TABLE kb.graph_objects 
    ADD COLUMN last_accessed_at TIMESTAMPTZ;

-- Create partial index for efficient queries on accessed objects
-- Partial index saves space by excluding NULL values (historical data)
CREATE INDEX idx_graph_objects_last_accessed
    ON kb.graph_objects(last_accessed_at DESC)
    WHERE last_accessed_at IS NOT NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS kb.idx_graph_objects_last_accessed;
ALTER TABLE kb.graph_objects DROP COLUMN IF EXISTS last_accessed_at;
-- +goose StatementEnd
