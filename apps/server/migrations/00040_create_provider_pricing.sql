-- +goose Up
-- +goose StatementBegin

-- Global provider pricing table (retail rates, synced daily)
-- Prices are per 1 million tokens (or equivalent units for media)
CREATE TABLE IF NOT EXISTS kb.provider_pricing (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    provider        VARCHAR(50) NOT NULL,   -- 'google-ai' or 'vertex-ai'
    model           VARCHAR(255) NOT NULL,  -- model name
    text_input_price    NUMERIC(12, 8) NOT NULL DEFAULT 0,  -- per 1M tokens
    image_input_price   NUMERIC(12, 8) NOT NULL DEFAULT 0,  -- per 1M tokens
    video_input_price   NUMERIC(12, 8) NOT NULL DEFAULT 0,  -- per 1M tokens
    audio_input_price   NUMERIC(12, 8) NOT NULL DEFAULT 0,  -- per 1M tokens
    output_price        NUMERIC(12, 8) NOT NULL DEFAULT 0,  -- per 1M tokens
    last_synced     TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT uq_provider_pricing UNIQUE (provider, model)
);

-- Organization-specific custom pricing overrides (enterprise rates)
CREATE TABLE IF NOT EXISTS kb.organization_custom_pricing (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    org_id          UUID NOT NULL REFERENCES kb.orgs(id) ON DELETE CASCADE,
    provider        VARCHAR(50) NOT NULL,
    model           VARCHAR(255) NOT NULL,
    text_input_price    NUMERIC(12, 8) NOT NULL DEFAULT 0,
    image_input_price   NUMERIC(12, 8) NOT NULL DEFAULT 0,
    video_input_price   NUMERIC(12, 8) NOT NULL DEFAULT 0,
    audio_input_price   NUMERIC(12, 8) NOT NULL DEFAULT 0,
    output_price        NUMERIC(12, 8) NOT NULL DEFAULT 0,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT uq_org_custom_pricing UNIQUE (org_id, provider, model)
);

CREATE INDEX IF NOT EXISTS idx_org_custom_pricing_org_id ON kb.organization_custom_pricing(org_id);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS kb.organization_custom_pricing;
DROP TABLE IF EXISTS kb.provider_pricing;
-- +goose StatementEnd
