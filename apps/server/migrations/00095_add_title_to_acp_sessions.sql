-- +goose Up

-- Add title field to ACP sessions for set_session_title built-in MCP tool
ALTER TABLE kb.acp_sessions ADD COLUMN IF NOT EXISTS title TEXT;

-- +goose Down

ALTER TABLE kb.acp_sessions DROP COLUMN IF EXISTS title;
