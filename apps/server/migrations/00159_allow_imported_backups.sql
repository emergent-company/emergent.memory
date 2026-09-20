-- +goose Up
-- Allow imported backups: the project_id column records the archive's SOURCE
-- project, which may live in another deployment and has no local kb.projects
-- row. Drop the FK to kb.projects (keep NOT NULL, always populated) and add an
-- explicit `imported` marker to disambiguate foreign archives.
ALTER TABLE kb.backups DROP CONSTRAINT IF EXISTS backups_project_id_fkey;
ALTER TABLE kb.backups ADD COLUMN IF NOT EXISTS imported BOOLEAN NOT NULL DEFAULT false;

COMMENT ON COLUMN kb.backups.imported IS 'True when the backup was imported from another deployment; project_id is the foreign source project (no local kb.projects row) and such backups are clone-restore only';

-- +goose Down
-- Revert to local-only backups: remove imported restores and backups, drop the
-- marker column, and restore the project_id FK.
DELETE FROM kb.restores
WHERE backup_id IN (SELECT id FROM kb.backups WHERE imported = true);

DELETE FROM kb.backups WHERE imported = true;

ALTER TABLE kb.backups DROP COLUMN IF EXISTS imported;

ALTER TABLE kb.backups
    ADD CONSTRAINT backups_project_id_fkey FOREIGN KEY (project_id) REFERENCES kb.projects(id) ON DELETE CASCADE;
