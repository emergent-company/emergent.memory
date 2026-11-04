-- +goose Up
-- +goose StatementBegin
ALTER TABLE kb.llm_usage_events
    ADD COLUMN IF NOT EXISTS user_id UUID REFERENCES core.user_profiles(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_llm_usage_events_user ON kb.llm_usage_events(user_id, created_at);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS kb.idx_llm_usage_events_user;
ALTER TABLE kb.llm_usage_events DROP COLUMN IF EXISTS user_id;
-- +goose StatementEnd
