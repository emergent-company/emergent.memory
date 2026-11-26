-- +goose Up
-- +goose StatementBegin

-- Persist the OpenTelemetry trace id of a discovery run on its job row, so the
-- admin surface (and operators) can fetch the run's span tree from Tempo the
-- same way extraction jobs do. NULL when tracing is disabled.
ALTER TABLE kb.discovery_jobs
    ADD COLUMN IF NOT EXISTS trace_id TEXT;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

ALTER TABLE kb.discovery_jobs DROP COLUMN IF EXISTS trace_id;

-- +goose StatementEnd
