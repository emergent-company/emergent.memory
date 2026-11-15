-- +goose Up
-- +goose StatementBegin
-- Dispatcher-written subject for verified GitHub source runs. Kept separate from
-- trigger_metadata because metadata is model-settable on spawned runs and must
-- never authorize the review tools.
ALTER TABLE kb.agent_runs ADD COLUMN IF NOT EXISTS source_subject JSONB;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE kb.agent_runs DROP COLUMN IF EXISTS source_subject;
-- +goose StatementEnd
