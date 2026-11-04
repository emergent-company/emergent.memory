-- +goose Up
-- GitHub-import source provenance on blueprint records: tracks where a blueprint
-- was imported from so later update checks can compare the stored manifest
-- against its upstream source. All three columns are nullable — only GitHub
-- imports populate them; non-imported blueprints leave them NULL.

ALTER TABLE kb.blueprints ADD COLUMN IF NOT EXISTS source_kind TEXT;
ALTER TABLE kb.blueprints ADD COLUMN IF NOT EXISTS source_url TEXT;
ALTER TABLE kb.blueprints ADD COLUMN IF NOT EXISTS source_ref TEXT;

-- +goose Down
ALTER TABLE kb.blueprints DROP COLUMN IF EXISTS source_ref;
ALTER TABLE kb.blueprints DROP COLUMN IF EXISTS source_url;
ALTER TABLE kb.blueprints DROP COLUMN IF EXISTS source_kind;
