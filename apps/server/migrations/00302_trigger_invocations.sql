-- +goose Up
-- +goose StatementBegin

-- Trigger-specific run configuration. Secrets are never stored here: a trigger
-- names a source credential that the executor mints at run start, and env vars
-- are restricted to a non-secret allowlist in the application layer.
ALTER TABLE kb.source_triggers
    ADD COLUMN IF NOT EXISTS invocation JSONB NOT NULL DEFAULT '{}'::jsonb;

-- Snapshot the invocation on the run so a later trigger edit cannot change a
-- queued run. source_trigger_id is audit only; the run does not depend on the
-- trigger row still existing.
ALTER TABLE kb.agent_runs
    ADD COLUMN IF NOT EXISTS run_invocation JSONB,
    ADD COLUMN IF NOT EXISTS source_trigger_id UUID;

CREATE INDEX IF NOT EXISTS idx_agent_runs_source_trigger
    ON kb.agent_runs (source_trigger_id)
    WHERE source_trigger_id IS NOT NULL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS kb.idx_agent_runs_source_trigger;
ALTER TABLE kb.agent_runs
    DROP COLUMN IF EXISTS source_trigger_id,
    DROP COLUMN IF EXISTS run_invocation;
ALTER TABLE kb.source_triggers
    DROP COLUMN IF EXISTS invocation;
-- +goose StatementEnd
