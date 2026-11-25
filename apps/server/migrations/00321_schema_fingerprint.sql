-- +goose Up
-- +goose StatementBegin

-- Schema fingerprint for extraction jobs: a hash of the project's registered
-- object/relationship type names at job creation, used by backfill to find
-- documents whose latest extraction predates a schema change.
ALTER TABLE kb.object_extraction_jobs
    ADD COLUMN IF NOT EXISTS schema_fingerprint TEXT;

CREATE INDEX IF NOT EXISTS idx_object_extraction_jobs_doc_fp
    ON kb.object_extraction_jobs (document_id, completed_at DESC)
    WHERE status = 'completed';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP INDEX IF EXISTS idx_object_extraction_jobs_doc_fp;
ALTER TABLE kb.object_extraction_jobs DROP COLUMN IF EXISTS schema_fingerprint;

-- +goose StatementEnd
