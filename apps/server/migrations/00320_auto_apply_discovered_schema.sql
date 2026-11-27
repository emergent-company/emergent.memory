-- +goose Up
-- +goose StatementBegin

-- Auto-apply bookkeeping for schema discovery: when a project runs in
-- pipeline_mode=auto, the discovery completion records what it applied (or
-- declined, and why) directly on the job so a retry or a duplicate run cannot
-- apply the same diff twice.
ALTER TABLE kb.discovery_jobs
    ADD COLUMN IF NOT EXISTS auto_applied BOOLEAN NOT NULL DEFAULT false;

ALTER TABLE kb.discovery_jobs
    ADD COLUMN IF NOT EXISTS applied_report JSONB NOT NULL DEFAULT '{}'::jsonb;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

ALTER TABLE kb.discovery_jobs DROP COLUMN IF EXISTS applied_report;
ALTER TABLE kb.discovery_jobs DROP COLUMN IF EXISTS auto_applied;

-- +goose StatementEnd
