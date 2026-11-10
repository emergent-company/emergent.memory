-- +goose Up
-- +goose StatementBegin

-- GitHub App review integration.
--
-- App credentials are App-level (one GitHub App can back many installations and
-- shares one private key and one webhook secret). Installations are the routing
-- unit: they bind an installation to a project, a repository allowlist, a
-- runtime review agent, and a queue.

CREATE TABLE IF NOT EXISTS core.github_apps (
    id                       UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id                   UUID NOT NULL,
    app_id                   BIGINT NOT NULL,
    slug                     TEXT NOT NULL DEFAULT '',
    private_key_encrypted    TEXT NOT NULL,
    webhook_secret_encrypted TEXT NOT NULL,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_github_apps_org_app
    ON core.github_apps (org_id, app_id);

CREATE TABLE IF NOT EXISTS core.github_app_installations (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    app_pk          UUID NOT NULL REFERENCES core.github_apps (id) ON DELETE CASCADE,
    org_id          UUID NOT NULL,
    project_id      UUID NOT NULL,
    installation_id BIGINT NOT NULL,
    repos           TEXT[] NOT NULL DEFAULT '{}',
    agent_id        UUID NOT NULL,
    queue           TEXT NOT NULL DEFAULT 'default',
    enabled         BOOLEAN NOT NULL DEFAULT TRUE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- One enabled installation row per GitHub installation. Disabled rows may be
-- kept for history without blocking a re-install.
CREATE UNIQUE INDEX IF NOT EXISTS uq_github_app_installations_enabled
    ON core.github_app_installations (installation_id)
    WHERE enabled;

CREATE INDEX IF NOT EXISTS idx_github_app_installations_project
    ON core.github_app_installations (project_id);

-- Ingress dedupe only. Several deliveries legitimately share a head SHA, so
-- per-head uniqueness lives in github_pr_review_claims, not here.
CREATE TABLE IF NOT EXISTS core.github_app_deliveries (
    delivery_id     TEXT PRIMARY KEY,
    installation_id BIGINT NOT NULL,
    repo            TEXT NOT NULL DEFAULT '',
    pull_request    INTEGER NOT NULL DEFAULT 0,
    head_sha        TEXT NOT NULL DEFAULT '',
    received_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- One active review per (installation, repo, PR, head SHA). A retry re-uses the
-- recorded run_id; a newer head inserts a new row and supersedes the old one.
CREATE TABLE IF NOT EXISTS core.github_pr_review_claims (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    installation_id BIGINT NOT NULL,
    repo            TEXT NOT NULL,
    pull_request    INTEGER NOT NULL,
    head_sha        TEXT NOT NULL,
    run_id          UUID,
    state           TEXT NOT NULL DEFAULT 'pending',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT chk_github_pr_review_claims_state
        CHECK (state IN ('pending', 'running', 'done', 'superseded', 'cancelled'))
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_github_pr_review_claims_head
    ON core.github_pr_review_claims (installation_id, repo, pull_request, head_sha);

CREATE INDEX IF NOT EXISTS idx_github_pr_review_claims_active
    ON core.github_pr_review_claims (installation_id, repo, pull_request, state);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE IF EXISTS core.github_pr_review_claims;
DROP TABLE IF EXISTS core.github_app_deliveries;
DROP TABLE IF EXISTS core.github_app_installations;
DROP TABLE IF EXISTS core.github_apps;

-- +goose StatementEnd
