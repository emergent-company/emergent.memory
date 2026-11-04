-- +goose Up
ALTER TABLE kb.llm_usage_events ADD COLUMN root_run_id uuid;

-- +goose Down
ALTER TABLE kb.llm_usage_events DROP COLUMN root_run_id;
