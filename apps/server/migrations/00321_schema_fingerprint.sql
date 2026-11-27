-- +goose Up
-- +goose StatementBegin

-- Schema generation stamp for extraction jobs: the project's schema_generation
-- at job creation, used by backfill to find documents whose latest extraction
-- predates a schema change (auto-apply bumps the project counter in P2). Using
-- the monotonic generation is simpler and correct where hashing registered type
-- names is not: the effective extraction schema is kb.project_schemas +
-- kb.graph_schemas, which the registry does not mirror.
ALTER TABLE kb.object_extraction_jobs
    ADD COLUMN IF NOT EXISTS schema_generation INTEGER;

-- +goose StatementEnd

-- +goose NO TRANSACTION
-- +goose StatementBegin
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_object_extraction_jobs_doc_gen
    ON kb.object_extraction_jobs (document_id, completed_at DESC)
    WHERE status = 'completed';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE kb.object_extraction_jobs DROP COLUMN IF EXISTS schema_generation;
-- +goose StatementEnd

-- +goose NO TRANSACTION
-- +goose StatementBegin
DROP INDEX CONCURRENTLY IF EXISTS kb.idx_object_extraction_jobs_doc_gen;
-- +goose StatementEnd
