-- +goose Up
-- +goose StatementBegin
ALTER TABLE kb.agent_definitions
    ADD COLUMN IF NOT EXISTS max_session_events integer;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE kb.agent_definitions
    DROP COLUMN IF EXISTS max_session_events;
-- +goose StatementEnd
