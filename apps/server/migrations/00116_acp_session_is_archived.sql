-- +goose Up
ALTER TABLE kb.acp_sessions
    ADD COLUMN IF NOT EXISTS is_archived BOOLEAN NOT NULL DEFAULT FALSE;

CREATE INDEX IF NOT EXISTS idx_acp_sessions_is_archived
    ON kb.acp_sessions (project_id, is_archived);

-- +goose Down
DROP INDEX IF EXISTS kb.idx_acp_sessions_is_archived;
ALTER TABLE kb.acp_sessions DROP COLUMN IF EXISTS is_archived;
