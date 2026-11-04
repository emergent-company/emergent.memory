-- +goose Up
-- +goose StatementBegin
-- Persist the OpenTelemetry trace id on object-extraction jobs so the admin
-- job-detail endpoint can fetch the job's span tree from Tempo for a trace
-- waterfall preview. NULL means the job ran with tracing disabled (no-op
-- tracer) or predates this column.
ALTER TABLE kb.object_extraction_jobs ADD COLUMN IF NOT EXISTS trace_id text;

CREATE INDEX IF NOT EXISTS idx_object_extraction_jobs_trace_id
    ON kb.object_extraction_jobs(trace_id) WHERE trace_id IS NOT NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS kb.idx_object_extraction_jobs_trace_id;
ALTER TABLE kb.object_extraction_jobs DROP COLUMN IF EXISTS trace_id;
-- +goose StatementEnd
