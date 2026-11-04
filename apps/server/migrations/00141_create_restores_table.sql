-- +goose Up
-- +goose StatementBegin
-- Restore job tracking for project backup restore (overwrite + clone modes).
CREATE TABLE IF NOT EXISTS kb.restores (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id   UUID NOT NULL REFERENCES kb.orgs(id) ON DELETE CASCADE,
    backup_id         UUID NOT NULL REFERENCES kb.backups(id) ON DELETE CASCADE,
    mode              TEXT NOT NULL,
    source_project_id UUID,
    target_project_id UUID,
    status            TEXT NOT NULL DEFAULT 'pending',
    progress          INTEGER NOT NULL DEFAULT 0,
    error_message     TEXT,
    stats             JSONB,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by        UUID REFERENCES core.user_profiles(id),
    completed_at      TIMESTAMPTZ,

    CONSTRAINT restores_mode_check CHECK (mode IN ('overwrite', 'clone')),
    CONSTRAINT restores_status_check CHECK (status IN ('pending', 'running', 'completed', 'failed')),
    CONSTRAINT restores_progress_check CHECK (progress >= 0 AND progress <= 100)
);

CREATE INDEX IF NOT EXISTS idx_restores_org ON kb.restores(organization_id);
CREATE INDEX IF NOT EXISTS idx_restores_status ON kb.restores(status);
CREATE INDEX IF NOT EXISTS idx_restores_backup ON kb.restores(backup_id);

COMMENT ON TABLE kb.restores IS 'Tracks project backup restore jobs (overwrite and clone modes)';
COMMENT ON COLUMN kb.restores.mode IS 'Restore mode: overwrite (same project) or clone (new project)';
COMMENT ON COLUMN kb.restores.stats IS 'Per-table restore statistics: {documents: 150, chunks: 3000, ...}';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS kb.restores CASCADE;
-- +goose StatementEnd
