-- +goose Up

-- GitHub App configuration for repository access
-- Stores encrypted credentials from GitHub App manifest flow or CLI setup.
-- At most one row per Emergent instance (singleton).
CREATE TABLE IF NOT EXISTS core.github_app_config (
    id                      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    app_id                  BIGINT NOT NULL,
    app_slug                TEXT NOT NULL DEFAULT '',
    private_key_encrypted   BYTEA NOT NULL,
    webhook_secret_encrypted BYTEA,
    client_id               TEXT NOT NULL DEFAULT '',
    client_secret_encrypted BYTEA,
    installation_id         BIGINT,
    installation_org        TEXT,
    owner_id                TEXT NOT NULL DEFAULT '',
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Ensure only one config exists per owner
CREATE UNIQUE INDEX IF NOT EXISTS idx_github_app_config_owner
    ON core.github_app_config (owner_id);

-- +goose Down

DROP TABLE IF EXISTS core.github_app_config;
