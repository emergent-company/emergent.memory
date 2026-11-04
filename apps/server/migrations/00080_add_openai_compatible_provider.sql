-- +goose Up
-- +goose StatementBegin
ALTER TABLE kb.org_provider_configs ADD COLUMN IF NOT EXISTS base_url TEXT;
ALTER TABLE kb.project_provider_configs ADD COLUMN IF NOT EXISTS base_url TEXT;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE kb.org_provider_configs DROP COLUMN IF EXISTS base_url;
ALTER TABLE kb.project_provider_configs DROP COLUMN IF EXISTS base_url;
-- +goose StatementEnd
