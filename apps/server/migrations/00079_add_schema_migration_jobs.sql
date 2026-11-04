-- +goose Up
CREATE TABLE IF NOT EXISTS kb.schema_migration_jobs (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    project_id      UUID NOT NULL,
    from_schema_id  UUID NOT NULL,
    to_schema_id    UUID NOT NULL,
    chain           JSONB NOT NULL DEFAULT '[]',
    status          TEXT NOT NULL DEFAULT 'pending',
    risk_level      TEXT,
    objects_migrated INT NOT NULL DEFAULT 0,
    objects_failed   INT NOT NULL DEFAULT 0,
    error            TEXT,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    started_at       TIMESTAMPTZ,
    completed_at     TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_schema_migration_jobs_project_id
    ON kb.schema_migration_jobs (project_id);

CREATE INDEX IF NOT EXISTS idx_schema_migration_jobs_status
    ON kb.schema_migration_jobs (status);

-- +goose Down
DROP TABLE IF EXISTS kb.schema_migration_jobs;
