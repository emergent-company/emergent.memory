-- +goose Up
-- Provider instances: a provider config row is now identified by a
-- project/org-scoped slug, and several instances may share one dialect. The
-- existing `provider` column keeps holding the dialect (it becomes the legacy
-- alias column until a later migration renames it).
--
-- Before this migration there are no custom slugs, so every stored model prefix
-- is a dialect name. Normalizing therefore strips only a recognised dialect
-- prefix and leaves unqualified multi-segment model ids (Vertex resource paths
-- such as `publishers/google/models/...`) untouched.

-- 1. Add the slug column and backfill it from the dialect (the default instance).
ALTER TABLE kb.project_provider_configs
  ADD COLUMN IF NOT EXISTS slug varchar(63);
ALTER TABLE kb.org_provider_configs
  ADD COLUMN IF NOT EXISTS slug varchar(63);

UPDATE kb.project_provider_configs SET slug = provider WHERE slug IS NULL OR slug = '';
UPDATE kb.org_provider_configs     SET slug = provider WHERE slug IS NULL OR slug = '';

ALTER TABLE kb.project_provider_configs ALTER COLUMN slug SET NOT NULL;
ALTER TABLE kb.org_provider_configs     ALTER COLUMN slug SET NOT NULL;

-- 2. Swap uniqueness: instance identity is (project|org, slug), no longer the
--    dialect. The old constraints were created inline and are unnamed, so they
--    are dropped by their PostgreSQL auto-generated names.
ALTER TABLE kb.project_provider_configs
  DROP CONSTRAINT IF EXISTS project_provider_configs_project_id_provider_key;
ALTER TABLE kb.org_provider_configs
  DROP CONSTRAINT IF EXISTS org_provider_configs_org_id_provider_key;

CREATE UNIQUE INDEX IF NOT EXISTS uq_project_provider_configs_project_slug
  ON kb.project_provider_configs (project_id, slug);
CREATE UNIQUE INDEX IF NOT EXISTS uq_org_provider_configs_org_slug
  ON kb.org_provider_configs (org_id, slug);

-- 3. Normalize already-prefixed model columns (strip a recognised dialect
--    prefix only).
UPDATE kb.project_provider_configs
SET generative_model = substr(generative_model, position('/' in generative_model) + 1)
WHERE generative_model LIKE '%/%'
  AND split_part(generative_model, '/', 1) IN ('google', 'google-vertex', 'openai', 'deepseek');

UPDATE kb.project_provider_configs
SET embedding_model = substr(embedding_model, position('/' in embedding_model) + 1)
WHERE embedding_model LIKE '%/%'
  AND split_part(embedding_model, '/', 1) IN ('google', 'google-vertex', 'openai', 'deepseek');

UPDATE kb.org_provider_configs
SET generative_model = substr(generative_model, position('/' in generative_model) + 1)
WHERE generative_model LIKE '%/%'
  AND split_part(generative_model, '/', 1) IN ('google', 'google-vertex', 'openai', 'deepseek');

UPDATE kb.org_provider_configs
SET embedding_model = substr(embedding_model, position('/' in embedding_model) + 1)
WHERE embedding_model LIKE '%/%'
  AND split_part(embedding_model, '/', 1) IN ('google', 'google-vertex', 'openai', 'deepseek');

-- +goose Down
-- Note: the model normalization above is lossy (the stripped dialect prefix is
-- not restored). Down restores the pre-instance uniqueness and drops the slug
-- column.
--
-- Rollback caveat: Down re-adds UNIQUE (project_id, provider), which FAILS
-- loudly if a second same-dialect instance was created after this migration
-- (two rows now share one dialect). Roll back before using multi-instance
-- providers, or delete the extra instances first.
DROP INDEX IF EXISTS uq_project_provider_configs_project_slug;
DROP INDEX IF EXISTS uq_org_provider_configs_org_slug;

ALTER TABLE kb.project_provider_configs
  ADD CONSTRAINT project_provider_configs_project_id_provider_key UNIQUE (project_id, provider);
ALTER TABLE kb.org_provider_configs
  ADD CONSTRAINT org_provider_configs_org_id_provider_key UNIQUE (org_id, provider);

ALTER TABLE kb.project_provider_configs DROP COLUMN IF EXISTS slug;
ALTER TABLE kb.org_provider_configs     DROP COLUMN IF EXISTS slug;
