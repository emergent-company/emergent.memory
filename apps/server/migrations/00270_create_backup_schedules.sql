-- +goose Up
-- +goose StatementBegin
-- Per-project recurring backup configuration. One row per project: a project
-- has at most one schedule (the UI models it as enable / edit / delete rather
-- than a list, and the runner's uniqueness assumption is what makes the
-- ON CONFLICT-free upsert in the repository safe).
CREATE TABLE IF NOT EXISTS kb.backup_schedules (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL REFERENCES kb.orgs(id) ON DELETE CASCADE,
    project_id      UUID NOT NULL REFERENCES kb.projects(id) ON DELETE CASCADE,

    enabled         BOOLEAN NOT NULL DEFAULT true,
    -- Standard 5-field cron ("minute hour day-of-month month day-of-week"),
    -- evaluated in UTC. Validated in Go on write; the column stays TEXT so a
    -- future expression dialect does not need a migration.
    cron_schedule   TEXT NOT NULL,
    timezone        TEXT NOT NULL DEFAULT 'UTC',

    retention_days  INTEGER NOT NULL DEFAULT 30,
    include_chat    BOOLEAN NOT NULL DEFAULT false,
    include_journal BOOLEAN NOT NULL DEFAULT false,
    include_deleted BOOLEAN NOT NULL DEFAULT false,

    -- Runner bookkeeping. next_run_at is the due-query key; last_run_at is
    -- stamped when the runner claims the row (the scheduled fire time), and
    -- last_backup_id points at the backup the last fire started.
    next_run_at     TIMESTAMPTZ NOT NULL,
    last_run_at     TIMESTAMPTZ,
    last_backup_id  UUID REFERENCES kb.backups(id) ON DELETE SET NULL,

    created_by      UUID REFERENCES core.user_profiles(id),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT current_timestamp,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT current_timestamp,

    CONSTRAINT backup_schedules_project_key UNIQUE (project_id),
    CONSTRAINT backup_schedules_retention_days_check
        CHECK (retention_days BETWEEN 1 AND 365)
);

-- The runner's only hot query is "enabled schedules that are due now". Partial
-- on enabled so disabled rows never enter the index scan.
CREATE INDEX IF NOT EXISTS idx_backup_schedules_due
    ON kb.backup_schedules (next_run_at)
    WHERE enabled;

COMMENT ON TABLE kb.backup_schedules IS
    'Per-project recurring backup schedule; one row per project, executed by the backups:project_schedules scheduler task';
COMMENT ON COLUMN kb.backup_schedules.cron_schedule IS
    'Standard 5-field cron expression evaluated in UTC, e.g. "0 2 * * *" for 02:00 UTC daily';
COMMENT ON COLUMN kb.backup_schedules.retention_days IS
    'Retention applied to every backup the schedule creates: expires_at = created_at + retention_days';
COMMENT ON COLUMN kb.backup_schedules.next_run_at IS
    'Next due instant; the runner claims rows where enabled AND next_run_at <= now()';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS kb.backup_schedules;
-- +goose StatementEnd
