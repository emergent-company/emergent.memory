-- +goose Up
-- Records the canonical hash of the RAW upstream manifest as fetched at import
-- time (BEFORE any project model overrides are applied to the stored manifest).
-- Update checks compare the freshly fetched upstream manifest against this
-- baseline, so a locally-overridden import (whose stored kb.blueprints.manifest
-- no longer matches upstream) does not report a permanent false-positive update.
-- NULL for non-imports and for imports predating this column.
ALTER TABLE kb.blueprints
    ADD COLUMN IF NOT EXISTS source_manifest_checksum TEXT;

-- +goose Down
ALTER TABLE kb.blueprints DROP COLUMN IF EXISTS source_manifest_checksum;
