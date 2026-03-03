-- +goose Up
-- +goose StatementBegin

-- Create enum type for provider policy
DO $$ BEGIN
    CREATE TYPE kb.provider_policy AS ENUM ('none', 'organization', 'project');
EXCEPTION
    WHEN duplicate_object THEN NULL;
END $$;

-- Project-level provider policies and optional credential/model overrides
CREATE TABLE IF NOT EXISTS kb.project_provider_policies (
    id               UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    project_id       UUID NOT NULL REFERENCES kb.projects(id) ON DELETE CASCADE,
    provider         VARCHAR(50) NOT NULL,  -- 'google-ai' or 'vertex-ai'
    policy           kb.provider_policy NOT NULL DEFAULT 'none',

    -- Project-level credential override (only used when policy = 'project')
    encrypted_credential BYTEA,
    encryption_nonce     BYTEA,

    -- Vertex AI metadata override (only used when policy = 'project' and provider = 'vertex-ai')
    gcp_project VARCHAR(255),
    location    VARCHAR(100),

    -- Project-level model overrides (only used when policy = 'project')
    embedding_model  VARCHAR(255),
    generative_model VARCHAR(255),

    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- One policy per provider per project
    CONSTRAINT uq_project_provider_policy UNIQUE (project_id, provider)
);

CREATE INDEX IF NOT EXISTS idx_project_provider_policies_project_id ON kb.project_provider_policies(project_id);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS kb.project_provider_policies;
DROP TYPE IF EXISTS kb.provider_policy;
-- +goose StatementEnd
