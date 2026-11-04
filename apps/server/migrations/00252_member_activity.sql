-- +goose Up
-- +goose StatementBegin
-- Per-user, per-day activity rollup backing the member activity indicators
-- (member-activity capability). One row per (day, user, org, project):
--
--   request_count  — authenticated requests seen by the auth middleware
--   action_count   — user-attributed domain actions derived from existing
--                    sources (project journal, LLM usage, chat conversations)
--   last_seen_at   — most recent observed request for that bucket
--   last_action_at — most recent derived domain action for that bucket
--
-- The row id is a deterministic UUID over the bucket key so every writer
-- (middleware flush, nightly rollup, backfill) can upsert idempotently.
--
-- No RLS: this is aggregate telemetry consumed only through the admin-gated
-- activity API. It matches the precedent set by kb.project_journal and
-- kb.llm_usage_events, which are also cross-tenant aggregate sources and carry
-- no row-level policy; reads are authorized at the API layer.
CREATE TABLE IF NOT EXISTS kb.user_activity_daily (
    id              uuid PRIMARY KEY,
    day             date NOT NULL,
    user_id         uuid NOT NULL REFERENCES core.user_profiles(id) ON DELETE CASCADE,
    organization_id uuid REFERENCES kb.orgs(id) ON DELETE CASCADE,
    project_id      uuid REFERENCES kb.projects(id) ON DELETE CASCADE,
    request_count   integer NOT NULL DEFAULT 0,
    action_count    integer NOT NULL DEFAULT 0,
    last_seen_at    timestamptz,
    last_action_at  timestamptz,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_user_activity_daily_org_day
    ON kb.user_activity_daily (organization_id, day DESC);

CREATE INDEX IF NOT EXISTS idx_user_activity_daily_project_day
    ON kb.user_activity_daily (project_id, day DESC);

CREATE INDEX IF NOT EXISTS idx_user_activity_daily_user_day
    ON kb.user_activity_daily (user_id, day DESC);

COMMENT ON TABLE kb.user_activity_daily IS
    'Per-user daily activity rollup for org/project member activity indicators';
COMMENT ON COLUMN kb.user_activity_daily.id IS
    'Deterministic UUID over (day,user_id,organization_id,project_id) for idempotent upserts';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS kb.user_activity_daily;
-- +goose StatementEnd
