-- +goose Up
-- Drop old tables (in dependency order: no FK deps between them)
DROP TABLE IF EXISTS kb.organization_provider_model_selections;
DROP TABLE IF EXISTS kb.organization_provider_credentials;
DROP TABLE IF EXISTS kb.project_provider_policies;

-- Drop provider_policy enum if it exists
DROP TYPE IF EXISTS kb.provider_policy;

-- Create kb.org_provider_configs
CREATE TABLE kb.org_provider_configs (
    id                   UUID         PRIMARY KEY DEFAULT uuid_generate_v4(),
    org_id               UUID         NOT NULL REFERENCES kb.orgs(id) ON DELETE CASCADE,
    provider             VARCHAR(50)  NOT NULL,
    encrypted_credential BYTEA        NOT NULL,
    encryption_nonce     BYTEA        NOT NULL,
    gcp_project          VARCHAR(255),
    location             VARCHAR(100),
    generative_model     VARCHAR(255),
    embedding_model      VARCHAR(255),
    created_at           TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at           TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    UNIQUE (org_id, provider)
);

-- Create kb.project_provider_configs
CREATE TABLE kb.project_provider_configs (
    id                   UUID         PRIMARY KEY DEFAULT uuid_generate_v4(),
    project_id           UUID         NOT NULL REFERENCES kb.projects(id) ON DELETE CASCADE,
    provider             VARCHAR(50)  NOT NULL,
    encrypted_credential BYTEA        NOT NULL,
    encryption_nonce     BYTEA        NOT NULL,
    gcp_project          VARCHAR(255),
    location             VARCHAR(100),
    generative_model     VARCHAR(255),
    embedding_model      VARCHAR(255),
    created_at           TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at           TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    UNIQUE (project_id, provider)
);

-- +goose Down
DROP TABLE IF EXISTS kb.project_provider_configs;
DROP TABLE IF EXISTS kb.org_provider_configs;
