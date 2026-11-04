-- +goose Up
-- Adds a grace period between marking a project for deletion and hard-purging
-- its row (and cascaded data). A durable scheduler sweeper purges projects
-- whose deletion_scheduled_for has elapsed; the project remains restorable
-- until then. Existing soft-deleted rows are scheduled immediately.
ALTER TABLE kb.projects ADD COLUMN deletion_scheduled_for timestamptz;

CREATE INDEX idx_projects_deletion_scheduled_for ON kb.projects USING btree (deletion_scheduled_for) WHERE (deletion_scheduled_for IS NOT NULL);

UPDATE kb.projects SET deletion_scheduled_for = COALESCE(deleted_at, now()) WHERE deleted_at IS NOT NULL AND deletion_scheduled_for IS NULL;

-- +goose Down
-- Restore pending-deletion projects to active before dropping the schedule
-- column, so a rollback does not leave them permanently soft-deleted with no
-- way to be purged.
UPDATE kb.projects SET deleted_at = NULL, deleted_by = NULL WHERE deletion_scheduled_for IS NOT NULL;

DROP INDEX IF EXISTS kb.idx_projects_deletion_scheduled_for;
ALTER TABLE kb.projects DROP COLUMN deletion_scheduled_for;
