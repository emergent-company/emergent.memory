-- +goose Up
CREATE TABLE IF NOT EXISTS kb.discovery_jobs (
    id                       UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id                UUID        NOT NULL,
    organization_id          UUID        NOT NULL,
    project_id               UUID        NOT NULL,
    status                   TEXT        NOT NULL,
    progress                 JSONB       NOT NULL DEFAULT '{}'::jsonb,
    config                   JSONB       NOT NULL DEFAULT '{}'::jsonb,
    kb_purpose               TEXT        NOT NULL DEFAULT '',
    discovered_types         JSONB       DEFAULT '[]'::jsonb,
    discovered_relationships JSONB       DEFAULT '[]'::jsonb,
    schema_id                UUID,
    error_message            TEXT,
    retry_count              INT         NOT NULL DEFAULT 0,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at               TIMESTAMPTZ,
    completed_at             TIMESTAMPTZ,
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS kb.discovery_type_candidates (
    id                     UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    job_id                 UUID        NOT NULL REFERENCES kb.discovery_jobs(id) ON DELETE CASCADE,
    batch_number           INT         NOT NULL,
    type_name              TEXT        NOT NULL,
    description            TEXT,
    confidence             REAL        NOT NULL,
    inferred_schema        JSONB       NOT NULL,
    example_instances      JSONB       DEFAULT '[]'::jsonb,
    frequency              INT         NOT NULL DEFAULT 1,
    proposed_relationships JSONB       DEFAULT '[]'::jsonb,
    source_document_ids    UUID[]      DEFAULT '{}'::uuid[],
    extraction_context     TEXT,
    refinement_iteration   INT         NOT NULL DEFAULT 1,
    merged_from            UUID[]      DEFAULT NULL,
    status                 TEXT        NOT NULL DEFAULT 'candidate',
    created_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at             TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_discovery_jobs_project_id ON kb.discovery_jobs(project_id);
CREATE INDEX IF NOT EXISTS idx_discovery_jobs_status ON kb.discovery_jobs(status);
CREATE INDEX IF NOT EXISTS idx_discovery_type_candidates_job_id ON kb.discovery_type_candidates(job_id);

-- +goose Down
DROP TABLE IF EXISTS kb.discovery_type_candidates;
DROP TABLE IF EXISTS kb.discovery_jobs;
