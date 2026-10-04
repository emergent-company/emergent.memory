-- +goose Up
-- +goose StatementBegin

-- Persist vendor-specific provider config fields (issue #1451).
--
-- The vendor registry declares ExtraFields (Azure OpenAI api_version, Volcengine
-- Ark region, LKEAP secret_key). They were surfaced read-only in the providers
-- UI and never stored. Non-secret values live in a jsonb map; secret values
-- (ExtraField.Secret) are encrypted with the same envelope as the primary
-- credential and stored as a second ciphertext+nonce pair so they are never
-- persisted in plaintext.
ALTER TABLE kb.project_provider_configs
    ADD COLUMN IF NOT EXISTS extra_config jsonb NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN IF NOT EXISTS extra_secret_encrypted bytea,
    ADD COLUMN IF NOT EXISTS extra_secret_nonce bytea;

ALTER TABLE kb.org_provider_configs
    ADD COLUMN IF NOT EXISTS extra_config jsonb NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN IF NOT EXISTS extra_secret_encrypted bytea,
    ADD COLUMN IF NOT EXISTS extra_secret_nonce bytea;

COMMENT ON COLUMN kb.project_provider_configs.extra_config IS 'Non-secret vendor-specific extra config as a field-key to value jsonb map (e.g. {"api_version": "2024-10-21"}).';
COMMENT ON COLUMN kb.project_provider_configs.extra_secret_encrypted IS 'AES-GCM ciphertext of the json object of secret extra config fields; NULL when none configured.';
COMMENT ON COLUMN kb.project_provider_configs.extra_secret_nonce IS 'Nonce for extra_secret_encrypted.';
COMMENT ON COLUMN kb.org_provider_configs.extra_config IS 'Non-secret vendor-specific extra config as a field-key to value jsonb map.';
COMMENT ON COLUMN kb.org_provider_configs.extra_secret_encrypted IS 'AES-GCM ciphertext of the json object of secret extra config fields; NULL when none configured.';
COMMENT ON COLUMN kb.org_provider_configs.extra_secret_nonce IS 'Nonce for extra_secret_encrypted.';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE kb.project_provider_configs
    DROP COLUMN IF EXISTS extra_secret_nonce,
    DROP COLUMN IF EXISTS extra_secret_encrypted,
    DROP COLUMN IF EXISTS extra_config;

ALTER TABLE kb.org_provider_configs
    DROP COLUMN IF EXISTS extra_secret_nonce,
    DROP COLUMN IF EXISTS extra_secret_encrypted,
    DROP COLUMN IF EXISTS extra_config;
-- +goose StatementEnd
