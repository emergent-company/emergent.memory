-- +goose Up
-- +goose StatementBegin
-- Add migration archive column to preserve dropped fields during schema migrations
ALTER TABLE kb.graph_objects 
ADD COLUMN IF NOT EXISTS migration_archive JSONB DEFAULT '[]'::jsonb;

COMMENT ON COLUMN kb.graph_objects.migration_archive IS 
'Archive of dropped properties from schema migrations. Each entry contains: from_version, to_version, timestamp, dropped_data';

-- Add index for querying objects with archived data
CREATE INDEX IF NOT EXISTS idx_graph_objects_has_archive 
ON kb.graph_objects ((migration_archive != '[]'::jsonb)) 
WHERE migration_archive != '[]'::jsonb;

-- Add migration tracking table
CREATE TABLE IF NOT EXISTS kb.schema_migration_runs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL,
    from_version TEXT NOT NULL,
    to_version TEXT NOT NULL,
    started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMPTZ,
    status TEXT NOT NULL CHECK (status IN ('running', 'completed', 'failed', 'rolled_back')),
    total_objects INT NOT NULL DEFAULT 0,
    successful INT NOT NULL DEFAULT 0,
    failed INT NOT NULL DEFAULT 0,
    skipped INT NOT NULL DEFAULT 0,
    with_warnings INT NOT NULL DEFAULT 0,
    risk_level TEXT NOT NULL DEFAULT 'unknown',
    dry_run BOOLEAN NOT NULL DEFAULT false,
    forced BOOLEAN NOT NULL DEFAULT false,
    confirmed_data_loss BOOLEAN NOT NULL DEFAULT false,
    error_summary JSONB
);

CREATE INDEX IF NOT EXISTS idx_migration_runs_project ON kb.schema_migration_runs(project_id);
CREATE INDEX IF NOT EXISTS idx_migration_runs_status ON kb.schema_migration_runs(status);
CREATE INDEX IF NOT EXISTS idx_migration_runs_started ON kb.schema_migration_runs(started_at DESC);

COMMENT ON TABLE kb.schema_migration_runs IS 
'Tracks all schema migration runs with statistics and outcomes';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_migration_runs_started;
DROP INDEX IF EXISTS idx_migration_runs_status;
DROP INDEX IF EXISTS idx_migration_runs_project;
DROP TABLE IF EXISTS kb.schema_migration_runs;

DROP INDEX IF EXISTS idx_graph_objects_has_archive;
ALTER TABLE kb.graph_objects DROP COLUMN IF EXISTS migration_archive;
-- +goose StatementEnd
