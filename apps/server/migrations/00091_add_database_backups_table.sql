-- +goose Up
-- +goose StatementBegin
CREATE TABLE kb.database_backups (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    status       TEXT NOT NULL DEFAULT 'pending',
    storage_key  TEXT,
    size_bytes   BIGINT,
    started_at   TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    error        TEXT,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_database_backups_created_at ON kb.database_backups (created_at DESC);
CREATE INDEX idx_database_backups_status ON kb.database_backups (status);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS kb.database_backups;
-- +goose StatementEnd
