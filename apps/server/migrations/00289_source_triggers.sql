-- +goose Up
-- +goose StatementBegin

-- Source-native GitHub App connection and generic source triggers.
--
-- App credentials are org-scoped (one GitHub App can back many project sources
-- and installations); triggers and events are project-scoped. This is the
-- foundation for the github_app provider; the legacy core.github_* tables stay
-- until the ingress/tools are ported.

CREATE TABLE IF NOT EXISTS kb.source_apps (
    id                       UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id                   UUID NOT NULL REFERENCES kb.orgs (id) ON DELETE CASCADE,
    provider                 TEXT NOT NULL,
    app_id                   BIGINT NOT NULL,
    slug                     TEXT NOT NULL DEFAULT '',
    -- Sealed with LLM_ENCRYPTION_KEY (same key as source credentials).
    private_key_encrypted    BYTEA NOT NULL,
    webhook_secret_encrypted BYTEA NOT NULL,
    -- Stable path segment for this App's webhook endpoint.
    webhook_key              UUID NOT NULL DEFAULT gen_random_uuid(),
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_source_apps_org_provider_app
    ON kb.source_apps (org_id, provider, app_id);
CREATE UNIQUE INDEX IF NOT EXISTS uq_source_apps_webhook_key
    ON kb.source_apps (webhook_key);

ALTER TABLE kb.sources
    ADD COLUMN IF NOT EXISTS app_id UUID REFERENCES kb.source_apps (id) ON DELETE CASCADE,
    ADD COLUMN IF NOT EXISTS external_account_id TEXT;

CREATE INDEX IF NOT EXISTS idx_sources_app_account
    ON kb.sources (app_id, external_account_id);

CREATE TABLE IF NOT EXISTS kb.source_triggers (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id   UUID NOT NULL REFERENCES kb.projects (id) ON DELETE CASCADE,
    source_id    UUID NOT NULL REFERENCES kb.sources (id) ON DELETE CASCADE,
    event_type   TEXT NOT NULL,
    agent_id     UUID NOT NULL REFERENCES kb.agents (id) ON DELETE CASCADE,
    queue        TEXT NOT NULL DEFAULT 'default',
    event_filter JSONB NOT NULL DEFAULT '{}'::jsonb,
    enabled      BOOLEAN NOT NULL DEFAULT TRUE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_source_triggers_source_event
    ON kb.source_triggers (source_id, event_type);
CREATE INDEX IF NOT EXISTS idx_source_triggers_project
    ON kb.source_triggers (project_id);

CREATE TABLE IF NOT EXISTS kb.source_events (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id   UUID NOT NULL REFERENCES kb.projects (id) ON DELETE CASCADE,
    source_id    UUID NOT NULL REFERENCES kb.sources (id) ON DELETE CASCADE,
    delivery_id  TEXT NOT NULL,
    event_type   TEXT NOT NULL,
    action       TEXT NOT NULL DEFAULT '',
    subject_type TEXT NOT NULL,
    subject_ref  TEXT NOT NULL,
    head_sha     TEXT NOT NULL DEFAULT '',
    payload      JSONB NOT NULL DEFAULT '{}'::jsonb,
    state        TEXT NOT NULL DEFAULT 'received',
    run_id       UUID,
    error        TEXT,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT chk_source_events_state
        CHECK (state IN ('received', 'dispatched', 'skipped', 'failed', 'superseded', 'cancelled'))
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_source_events_delivery
    ON kb.source_events (source_id, delivery_id);
CREATE INDEX IF NOT EXISTS idx_source_events_subject
    ON kb.source_events (source_id, subject_ref);

-- One active claim per subject/head replaces core.github_pr_review_claims.
-- Deliveries stay in source_events (one row per delivery id); claims are the
-- idempotency/routing unit.
CREATE TABLE IF NOT EXISTS kb.source_event_claims (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id   UUID NOT NULL REFERENCES kb.projects (id) ON DELETE CASCADE,
    source_id    UUID NOT NULL REFERENCES kb.sources (id) ON DELETE CASCADE,
    subject_ref  TEXT NOT NULL,
    head_sha     TEXT NOT NULL,
    run_id       UUID,
    state        TEXT NOT NULL DEFAULT 'pending',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT chk_source_event_claims_state
        CHECK (state IN ('pending', 'running', 'done', 'superseded', 'cancelled'))
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_source_event_claims_head
    ON kb.source_event_claims (source_id, subject_ref, head_sha);
CREATE INDEX IF NOT EXISTS idx_source_event_claims_active
    ON kb.source_event_claims (source_id, subject_ref, state);

-- Project RLS mirrors kb.sources: an empty app.current_project_id is the
-- system/worker bypass, otherwise rows are scoped to the setting.
ALTER TABLE kb.source_triggers ENABLE ROW LEVEL SECURITY;
CREATE POLICY source_triggers_all ON kb.source_triggers FOR ALL USING (
    (COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text)
    OR ((project_id)::text = current_setting('app.current_project_id'::text, true))
) WITH CHECK (
    (COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text)
    OR ((project_id)::text = current_setting('app.current_project_id'::text, true))
);

ALTER TABLE kb.source_events ENABLE ROW LEVEL SECURITY;
CREATE POLICY source_events_all ON kb.source_events FOR ALL USING (
    (COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text)
    OR ((project_id)::text = current_setting('app.current_project_id'::text, true))
) WITH CHECK (
    (COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text)
    OR ((project_id)::text = current_setting('app.current_project_id'::text, true))
);

ALTER TABLE kb.source_event_claims ENABLE ROW LEVEL SECURITY;
CREATE POLICY source_event_claims_all ON kb.source_event_claims FOR ALL USING (
    (COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text)
    OR ((project_id)::text = current_setting('app.current_project_id'::text, true))
) WITH CHECK (
    (COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text)
    OR ((project_id)::text = current_setting('app.current_project_id'::text, true))
);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE IF EXISTS kb.source_event_claims;
DROP TABLE IF EXISTS kb.source_events;
DROP TABLE IF EXISTS kb.source_triggers;
DROP INDEX IF EXISTS kb.idx_sources_app_account;
ALTER TABLE kb.sources DROP COLUMN IF EXISTS external_account_id;
ALTER TABLE kb.sources DROP COLUMN IF EXISTS app_id;
DROP TABLE IF EXISTS kb.source_apps;

-- +goose StatementEnd
