-- +goose Up
-- Per-project flag: when true, a source sync that creates documents also starts
-- a schema-discovery job. It defaults to true so existing projects keep the
-- behaviour they have had since the sync worker first delegated discovery.
-- Turning it off leaves discovery available only through the explicit start /
-- retry endpoints (Schema → Schema discovery and the document page).
ALTER TABLE kb.projects ADD COLUMN auto_discovery_enabled boolean NOT NULL DEFAULT true;

-- +goose Down
ALTER TABLE kb.projects DROP COLUMN auto_discovery_enabled;
