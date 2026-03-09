-- +goose Up
-- +goose StatementBegin

-- Add trigger_message to agent_runs: carries the wakeup message injected as
-- the user message when a queued run is picked up by a worker.
-- Populated when a parent enqueues a child (with task instructions) or when
-- the server re-enqueues a parent after a child completes (with the child's result).
ALTER TABLE kb.agent_runs
    ADD COLUMN trigger_message TEXT;

COMMENT ON COLUMN kb.agent_runs.trigger_message IS
    'Optional message injected as the user message when this run starts. '
    'Set by trigger_agent (task instructions to child) or by the server on '
    'parent re-enqueue (child completion result). Overrides the agent default prompt.';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

ALTER TABLE kb.agent_runs DROP COLUMN IF EXISTS trigger_message;

-- +goose StatementEnd
