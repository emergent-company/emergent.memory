-- +goose Up
-- +goose StatementBegin

-- Which path rebuilt the session: 'load' (the pre-run sliding-window /
-- compression pass) or 'overflow' (a mid-run context-window rejection that was
-- summarised and retried once, #1138). Operators can tell the two apart without
-- inferring it from event counts.
ALTER TABLE kb.agent_session_compactions
    ADD COLUMN IF NOT EXISTS trigger TEXT NOT NULL DEFAULT 'load';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

ALTER TABLE kb.agent_session_compactions DROP COLUMN IF EXISTS trigger;

-- +goose StatementEnd
