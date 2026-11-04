-- +goose Up

-- Workspace provisioning is a phase of a run's working status, not a separate
-- session-level status. The column was write-only and stored per run despite its
-- name; drop it.
ALTER TABLE kb.agent_runs DROP COLUMN IF EXISTS session_status;

-- +goose Down

ALTER TABLE kb.agent_runs ADD COLUMN IF NOT EXISTS session_status VARCHAR(20) NOT NULL DEFAULT 'active';
