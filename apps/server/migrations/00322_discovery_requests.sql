-- +goose Up
-- +goose StatementBegin

-- Durable discovery request queue: a sync enqueues the document ids it
-- ingested (including new versions) instead of calling StartDiscovery directly,
-- so a 409 in-flight collision or a crash no longer drops those documents. A
-- background runner drains each project's queued ids into a discovery run.
CREATE TABLE IF NOT EXISTS kb.discovery_requests (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id   UUID NOT NULL REFERENCES kb.projects(id),
    document_ids UUID[] NOT NULL DEFAULT '{}',
    status       TEXT NOT NULL DEFAULT 'queued',
    attempts     INTEGER NOT NULL DEFAULT 0,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS uidx_discovery_requests_active
    ON kb.discovery_requests (project_id)
    WHERE status IN ('queued', 'running');

CREATE INDEX IF NOT EXISTS idx_discovery_requests_status
    ON kb.discovery_requests (status, updated_at);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE IF EXISTS kb.discovery_requests;

-- +goose StatementEnd
