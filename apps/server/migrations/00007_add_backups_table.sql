-- +goose Up
-- +goose StatementBegin
-- Create kb.backups table for project backup management
CREATE TABLE IF NOT EXISTS kb.backups (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL REFERENCES kb.orgs(id) ON DELETE CASCADE,
    project_id UUID NOT NULL REFERENCES kb.projects(id) ON DELETE CASCADE,
    project_name TEXT NOT NULL,
    
    -- Storage
    storage_key TEXT NOT NULL,
    size_bytes BIGINT NOT NULL DEFAULT 0,
    
    -- Status
    status TEXT NOT NULL DEFAULT 'creating',
    progress INTEGER NOT NULL DEFAULT 0,
    error_message TEXT,
    
    -- Metadata
    backup_type TEXT NOT NULL DEFAULT 'full',
    includes JSONB NOT NULL DEFAULT '{}',
    
    -- Statistics
    stats JSONB,
    
    -- Lifecycle
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by UUID REFERENCES core.user_profiles(id),
    completed_at TIMESTAMPTZ,
    expires_at TIMESTAMPTZ,
    deleted_at TIMESTAMPTZ,
    
    -- Checksums
    manifest_checksum TEXT,
    content_checksum TEXT,
    
    -- Future: Incremental backup support
    parent_backup_id UUID REFERENCES kb.backups(id),
    baseline_backup_id UUID REFERENCES kb.backups(id),
    change_window JSONB,
    
    -- Constraints
    CONSTRAINT backups_status_check CHECK (status IN ('creating', 'ready', 'failed', 'deleted')),
    CONSTRAINT backups_backup_type_check CHECK (backup_type IN ('full', 'incremental')),
    CONSTRAINT backups_progress_check CHECK (progress >= 0 AND progress <= 100)
);

-- Indexes for common queries
CREATE INDEX IF NOT EXISTS idx_backups_org_project ON kb.backups(organization_id, project_id) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_backups_status ON kb.backups(status) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_backups_expires ON kb.backups(expires_at) WHERE deleted_at IS NULL AND expires_at IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_backups_created ON kb.backups(created_at DESC) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_backups_parent ON kb.backups(parent_backup_id) WHERE parent_backup_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_backups_baseline ON kb.backups(baseline_backup_id) WHERE baseline_backup_id IS NOT NULL;

-- Comment on table
COMMENT ON TABLE kb.backups IS 'Stores metadata for project backups stored in MinIO';
COMMENT ON COLUMN kb.backups.storage_key IS 'MinIO object key: backups/{orgId}/{backupId}/backup.zip';
COMMENT ON COLUMN kb.backups.backup_type IS 'Type of backup: full (complete snapshot) or incremental (changes only)';
COMMENT ON COLUMN kb.backups.includes IS 'What data is included: {documents: true, chat: true, graph: true}';
COMMENT ON COLUMN kb.backups.stats IS 'Backup statistics: {documents: 150, chunks: 3000, files: 150, ...}';
COMMENT ON COLUMN kb.backups.change_window IS 'For incremental backups: {from: timestamp, to: timestamp}';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS kb.backups CASCADE;
-- +goose StatementEnd
