-- +goose Up
-- +goose StatementBegin
ALTER TABLE kb.provider_supported_models
    ADD COLUMN IF NOT EXISTS max_input_tokens integer;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE kb.provider_supported_models
    DROP COLUMN IF EXISTS max_input_tokens;
-- +goose StatementEnd
