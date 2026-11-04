-- +goose Up
-- +goose StatementBegin

-- Add dispatch_mode to kb.agent_definitions to control how the runtime schedules execution.
-- 'sync' (default) = blocking inline execution (current behaviour).
-- 'queued' = enqueued for worker pool execution; returns run_id immediately.
ALTER TABLE kb.agent_definitions
    ADD COLUMN dispatch_mode TEXT NOT NULL DEFAULT 'sync';

COMMENT ON COLUMN kb.agent_definitions.dispatch_mode IS 'Dispatch mode: sync (blocking, default) or queued (worker pool)';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

ALTER TABLE kb.agent_definitions DROP COLUMN IF EXISTS dispatch_mode;

-- +goose StatementEnd
