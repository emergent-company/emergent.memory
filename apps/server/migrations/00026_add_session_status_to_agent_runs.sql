-- +goose Up
-- Add session_status to kb.agent_runs to track workspace provisioning lifecycle.
-- Values: provisioning (workspace being set up), active (running), completed, error.
-- This is distinct from the 'status' column which tracks run execution state.
ALTER TABLE kb.agent_runs ADD COLUMN session_status VARCHAR(20) NOT NULL DEFAULT 'active';

-- Backfill: set completed runs to appropriate session status
UPDATE kb.agent_runs SET session_status = 'completed' WHERE status IN ('success', 'skipped', 'cancelled');
UPDATE kb.agent_runs SET session_status = 'error' WHERE status = 'error';

-- +goose Down
ALTER TABLE kb.agent_runs DROP COLUMN IF EXISTS session_status;
