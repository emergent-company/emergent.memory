-- +goose Up
-- +goose StatementBegin

-- Extraction quarantine: when strict typing drops objects/relationships because
-- their type is not in the installed schema, they are recorded here (when
-- apply.quarantine_unknown_types is on) instead of being silently discarded, so
-- a reviewer can see what a run leaked and re-extract it after a later discovery
-- run installs the missing type.
CREATE TABLE IF NOT EXISTS kb.extraction_quarantine (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id   UUID NOT NULL REFERENCES kb.projects(id),
    document_id  UUID,
    job_id       UUID,
    kind         TEXT NOT NULL,
    type_name    TEXT NOT NULL,
    payload      JSONB NOT NULL DEFAULT '{}'::jsonb,
    reason       TEXT NOT NULL DEFAULT 'unknown_type',
    status       TEXT NOT NULL DEFAULT 'open',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_at  TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_extraction_quarantine_project_status
    ON kb.extraction_quarantine (project_id, status, created_at);

CREATE INDEX IF NOT EXISTS idx_extraction_quarantine_type
    ON kb.extraction_quarantine (type_name);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE IF EXISTS kb.extraction_quarantine;

-- +goose StatementEnd
