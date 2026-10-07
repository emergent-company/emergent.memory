-- +goose Up
-- Blueprint update-check persistence: durable job queue (check|update) and the
-- per-(project, blueprint) check result, plus project-scoped GitHub PAT storage.
-- Only `check` is implemented in this change; the table shape supports the
-- later `update` (apply) action.

-- Per-(project, blueprint_name) check result. One row per applied, GitHub-imported
-- blueprint; upserted on every successful (or failed) check.
CREATE TABLE IF NOT EXISTS kb.blueprint_update_checks (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    project_id UUID NOT NULL,
    blueprint_name TEXT NOT NULL,
    source_url TEXT,
    source_ref TEXT,
    latest_version TEXT,
    latest_checksum TEXT,
    update_available BOOLEAN NOT NULL DEFAULT false,
    last_checked_at TIMESTAMPTZ,
    last_check_error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT blueprint_update_checks_project_name_key UNIQUE (project_id, blueprint_name)
);
CREATE INDEX IF NOT EXISTS idx_blueprint_update_checks_project
    ON kb.blueprint_update_checks (project_id);

-- Durable job queue for update checks (and future updates). Claimed with
-- SELECT ... FOR UPDATE SKIP LOCKED keyed by (status, created_at).
CREATE TABLE IF NOT EXISTS kb.blueprint_update_jobs (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    project_id UUID NOT NULL,
    blueprint_name TEXT NOT NULL,
    action TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    attempts INT NOT NULL DEFAULT 0,
    last_error TEXT,
    result JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at TIMESTAMPTZ,
    finished_at TIMESTAMPTZ,
    CONSTRAINT blueprint_update_jobs_action_check
        CHECK (action IN ('check', 'update')),
    CONSTRAINT blueprint_update_jobs_status_check
        CHECK (status IN ('pending', 'running', 'succeeded', 'failed'))
);
CREATE INDEX IF NOT EXISTS idx_blueprint_update_jobs_status_created
    ON kb.blueprint_update_jobs (status, created_at);

-- Project-scoped GitHub personal access token, encrypted at rest.
CREATE TABLE IF NOT EXISTS kb.github_credentials (
    project_id UUID PRIMARY KEY,
    encrypted_token BYTEA NOT NULL,
    nonce BYTEA NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE IF EXISTS kb.github_credentials;
DROP TABLE IF EXISTS kb.blueprint_update_jobs;
DROP TABLE IF EXISTS kb.blueprint_update_checks;
