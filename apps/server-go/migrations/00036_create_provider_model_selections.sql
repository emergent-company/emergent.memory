-- +goose Up
-- +goose StatementBegin

-- Organization-level default model selections per provider
CREATE TABLE IF NOT EXISTS kb.organization_provider_model_selections (
    id               UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    org_id           UUID NOT NULL REFERENCES kb.orgs(id) ON DELETE CASCADE,
    provider         VARCHAR(50) NOT NULL,  -- 'google-ai' or 'vertex-ai'
    embedding_model  VARCHAR(255),          -- selected default embedding model
    generative_model VARCHAR(255),          -- selected default generative model
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- One model selection per provider per organization
    CONSTRAINT uq_org_provider_model_selection UNIQUE (org_id, provider)
);

CREATE INDEX IF NOT EXISTS idx_org_provider_model_sel_org_id ON kb.organization_provider_model_selections(org_id);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS kb.organization_provider_model_selections;
-- +goose StatementEnd
