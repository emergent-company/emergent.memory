-- +goose Up
-- +goose StatementBegin

-- kb.org_model_config stores the explicit default generative and embedding
-- model for an organization. This is separate from provider credentials
-- (kb.org_provider_configs) so that credential setup and model selection
-- are independent concerns.
CREATE TABLE kb.org_model_config (
    org_id           UUID PRIMARY KEY REFERENCES kb.orgs(id) ON DELETE CASCADE,
    generative_model TEXT NOT NULL DEFAULT '',
    embedding_model  TEXT NOT NULL DEFAULT '',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- kb.project_model_config stores the explicit default generative and embedding
-- model for a project. Takes precedence over org_model_config.
CREATE TABLE kb.project_model_config (
    project_id       UUID PRIMARY KEY REFERENCES kb.projects(id) ON DELETE CASCADE,
    generative_model TEXT NOT NULL DEFAULT '',
    embedding_model  TEXT NOT NULL DEFAULT '',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS kb.project_model_config;
DROP TABLE IF EXISTS kb.org_model_config;
-- +goose StatementEnd
