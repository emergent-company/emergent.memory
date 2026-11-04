-- +goose Up
-- Record the provider instance (slug) and dialect on agent runs. `provider`
-- keeps holding the dialect for compatibility; provider_slug identifies the
-- instance. Legacy rows with a NULL provider stay NULL (labelled "legacy")
-- rather than being folded into an empty bucket.
ALTER TABLE kb.agent_runs
  ADD COLUMN IF NOT EXISTS provider_slug varchar(63);
ALTER TABLE kb.agent_runs
  ADD COLUMN IF NOT EXISTS dialect varchar(50);

UPDATE kb.agent_runs
SET provider_slug = provider, dialect = provider
WHERE provider IS NOT NULL AND provider <> '' AND provider_slug IS NULL;

-- +goose Down
ALTER TABLE kb.agent_runs DROP COLUMN IF EXISTS dialect;
ALTER TABLE kb.agent_runs DROP COLUMN IF EXISTS provider_slug;
