-- +goose Up
-- +goose StatementBegin
ALTER TABLE kb.object_extraction_jobs
    ADD COLUMN IF NOT EXISTS created_object_ids uuid[] NOT NULL DEFAULT '{}';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE kb.object_extraction_jobs
    DROP COLUMN IF EXISTS created_object_ids;
-- +goose StatementEnd
