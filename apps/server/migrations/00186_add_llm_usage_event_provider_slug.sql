-- +goose Up
-- Record the provider instance (slug) that served each LLM usage event, so two
-- instances of one dialect are not merged in usage reporting. The `provider`
-- column keeps holding the dialect.
ALTER TABLE kb.llm_usage_events
  ADD COLUMN IF NOT EXISTS provider_slug varchar(63);

-- Backfill: before instances existed the dialect was the only identity, so the
-- default instance slug equals the dialect. Unknown/empty dialects stay NULL
-- (legacy rows) rather than being folded into a bogus bucket.
UPDATE kb.llm_usage_events
SET provider_slug = provider
WHERE provider_slug IS NULL AND provider IS NOT NULL AND provider <> '';

CREATE INDEX IF NOT EXISTS idx_llm_usage_events_provider_slug
  ON kb.llm_usage_events(provider_slug, model, created_at);

-- +goose Down
DROP INDEX IF EXISTS idx_llm_usage_events_provider_slug;
ALTER TABLE kb.llm_usage_events DROP COLUMN IF EXISTS provider_slug;
