-- +goose Up
-- Add is_system flag to embedding_policies to mark system-managed policies
-- that users cannot delete.
ALTER TABLE kb.embedding_policies ADD COLUMN IF NOT EXISTS is_system boolean DEFAULT false NOT NULL;

-- Create an index for filtering system policies efficiently.
CREATE INDEX IF NOT EXISTS idx_embedding_policies_is_system ON kb.embedding_policies (project_id, is_system) WHERE is_system = true;

-- +goose Down
DROP INDEX IF EXISTS kb.idx_embedding_policies_is_system;
ALTER TABLE kb.embedding_policies DROP COLUMN IF EXISTS is_system;
