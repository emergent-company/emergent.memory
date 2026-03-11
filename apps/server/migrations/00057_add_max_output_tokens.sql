-- +goose Up
ALTER TABLE kb.provider_supported_models
    ADD COLUMN IF NOT EXISTS max_output_tokens INT;

-- +goose Down
ALTER TABLE kb.provider_supported_models
    DROP COLUMN IF EXISTS max_output_tokens;
