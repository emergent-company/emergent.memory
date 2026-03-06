-- +goose Up
-- +goose StatementBegin

-- Cache of supported models fetched from provider APIs
CREATE TABLE IF NOT EXISTS kb.provider_supported_models (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    provider    VARCHAR(50) NOT NULL,   -- 'google-ai' or 'vertex-ai'
    model_name  VARCHAR(255) NOT NULL,  -- e.g. 'gemini-2.0-flash', 'gemini-embedding-001'
    model_type  VARCHAR(50) NOT NULL,   -- 'embedding' or 'generative'
    display_name VARCHAR(255),          -- human-readable name from API
    last_synced TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- One entry per provider + model name
    CONSTRAINT uq_provider_model UNIQUE (provider, model_name)
);

CREATE INDEX IF NOT EXISTS idx_provider_supported_models_provider ON kb.provider_supported_models(provider);
CREATE INDEX IF NOT EXISTS idx_provider_supported_models_type ON kb.provider_supported_models(provider, model_type);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS kb.provider_supported_models;
-- +goose StatementEnd
