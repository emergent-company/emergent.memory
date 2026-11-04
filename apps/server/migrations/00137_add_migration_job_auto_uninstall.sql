-- +goose Up
-- The migration worker processes jobs asynchronously, so the auto_uninstall
-- flag must survive the persist/fetch round-trip. Previously it was a
-- runtime-only struct field (bun:"-") and was silently dropped, so the
-- from_version assignment was never deactivated after an upgrade.
ALTER TABLE kb.schema_migration_jobs
    ADD COLUMN auto_uninstall BOOLEAN NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE kb.schema_migration_jobs
    DROP COLUMN auto_uninstall;
