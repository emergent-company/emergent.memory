-- +goose Up
-- +goose StatementBegin

-- Knowledge-pipeline policy (auto schema discovery / auto-apply / extraction /
-- merge). pipeline_mode is the human-facing selector; pipeline_policy stores the
-- fully-resolved policy JSON (see domain/pipeline). schema_generation is a
-- monotonic counter bumped whenever the effective installed schema set changes,
-- used later to fingerprint extraction jobs for incremental backfill.
ALTER TABLE kb.projects
    ADD COLUMN IF NOT EXISTS pipeline_mode TEXT NOT NULL DEFAULT 'suggest';

ALTER TABLE kb.projects
    ADD COLUMN IF NOT EXISTS pipeline_policy JSONB NOT NULL DEFAULT '{}'::jsonb;

ALTER TABLE kb.projects
    ADD COLUMN IF NOT EXISTS schema_generation INTEGER NOT NULL DEFAULT 0;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

ALTER TABLE kb.projects DROP COLUMN IF EXISTS schema_generation;
ALTER TABLE kb.projects DROP COLUMN IF EXISTS pipeline_policy;
ALTER TABLE kb.projects DROP COLUMN IF EXISTS pipeline_mode;

-- +goose StatementEnd
