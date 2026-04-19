-- +goose Up
ALTER TABLE kb.llm_usage_events ADD COLUMN IF NOT EXISTS cached_tokens bigint NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE kb.llm_usage_events DROP COLUMN IF EXISTS cached_tokens;
