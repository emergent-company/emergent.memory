-- +goose Up
-- +goose StatementBegin
ALTER TABLE kb.llm_call_logs DROP COLUMN IF EXISTS langfuse_observation_id;
ALTER TABLE kb.system_process_logs DROP COLUMN IF EXISTS langfuse_trace_id;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE kb.llm_call_logs ADD COLUMN IF NOT EXISTS langfuse_observation_id text;
ALTER TABLE kb.system_process_logs ADD COLUMN IF NOT EXISTS langfuse_trace_id text;
-- +goose StatementEnd
