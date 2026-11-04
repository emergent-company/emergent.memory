-- +goose Up
-- +goose StatementBegin

-- Organization-level provider credentials (encrypted at rest)
CREATE TABLE IF NOT EXISTS kb.organization_provider_credentials (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    org_id      UUID NOT NULL REFERENCES kb.orgs(id) ON DELETE CASCADE,
    provider    VARCHAR(50) NOT NULL,  -- 'google-ai' or 'vertex-ai'
    
    -- Encrypted credential blob (AES-GCM encrypted API key or service account JSON)
    encrypted_credential BYTEA NOT NULL,
    -- Nonce used for AES-GCM encryption
    encryption_nonce     BYTEA NOT NULL,

    -- Vertex AI metadata (nullable, only used for vertex-ai provider)
    gcp_project VARCHAR(255),
    location    VARCHAR(100),

    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- One credential set per provider per organization
    CONSTRAINT uq_org_provider_credential UNIQUE (org_id, provider)
);

CREATE INDEX IF NOT EXISTS idx_org_provider_creds_org_id ON kb.organization_provider_credentials(org_id);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS kb.organization_provider_credentials;
-- +goose StatementEnd
