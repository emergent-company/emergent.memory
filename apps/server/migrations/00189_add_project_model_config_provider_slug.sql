-- +goose Up
-- Structured default-model selection for kb.project_model_config.
--
-- Before this migration the model columns hold a routed name ("openai/gpt-4o"),
-- a bare name ("gpt-4o"), or an unqualified multi-segment model id (a Vertex
-- resource path such as "publishers/google/models/gemini-2.5-flash"). This
-- migration makes the selection structured: the model columns are normalized to
-- hold the BARE model name and a new provider_slug column carries the instance
-- identity.
--
-- kb.project_model_config records no dialect today, so a bare value cannot fall
-- back to "the row's dialect". Each row is therefore resolved deterministically
-- (design D6):
--   (1) a first-"/"-segment that is a recognised dialect OR an existing project
--       instance slug names that instance, and the prefix is stripped;
--   (2) otherwise, if exactly one of the project's provider configs carries that
--       model name in its own (already-normalized) model column, that config's
--       slug is used;
--   (3) otherwise the row is flagged for manual resolution — never silently left
--       with a NULL slug, and never blind-split into a corrupted name.
--
-- Bare Vertex resource paths are matched by rule (2), never split at the first
-- "/"; an ambiguous multi-instance row (the same model name under two slugs) is
-- flagged, not silently assigned.

-- 1. Structured columns plus an explicit manual-resolution flag (rule 3).
ALTER TABLE kb.project_model_config
  ADD COLUMN IF NOT EXISTS generative_provider_slug varchar(63);
ALTER TABLE kb.project_model_config
  ADD COLUMN IF NOT EXISTS embedding_provider_slug varchar(63);
ALTER TABLE kb.project_model_config
  ADD COLUMN IF NOT EXISTS generative_slug_review_required boolean NOT NULL DEFAULT false;
ALTER TABLE kb.project_model_config
  ADD COLUMN IF NOT EXISTS embedding_slug_review_required boolean NOT NULL DEFAULT false;

-- 2. Rule (1a): a recognised dialect prefix names the dialect's default
--    instance (slug == dialect). Strip the prefix so the model column holds the
--    bare name.
UPDATE kb.project_model_config
SET generative_provider_slug = split_part(generative_model, '/', 1),
    generative_model         = substr(generative_model, position('/' in generative_model) + 1)
WHERE generative_model LIKE '%/%'
  AND split_part(generative_model, '/', 1) IN ('google', 'google-vertex', 'openai', 'deepseek');

UPDATE kb.project_model_config
SET embedding_provider_slug = split_part(embedding_model, '/', 1),
    embedding_model         = substr(embedding_model, position('/' in embedding_model) + 1)
WHERE embedding_model LIKE '%/%'
  AND split_part(embedding_model, '/', 1) IN ('google', 'google-vertex', 'openai', 'deepseek');

-- 3. Rule (1b): a first segment that is an existing project instance slug (but
--    not a dialect) names that instance; strip the prefix.
UPDATE kb.project_model_config pmc
SET generative_provider_slug = split_part(pmc.generative_model, '/', 1),
    generative_model         = substr(pmc.generative_model, position('/' in pmc.generative_model) + 1)
FROM kb.project_provider_configs ppc
WHERE ppc.project_id = pmc.project_id
  AND ppc.slug = split_part(pmc.generative_model, '/', 1)
  AND pmc.generative_provider_slug IS NULL
  AND pmc.generative_model LIKE '%/%';

UPDATE kb.project_model_config pmc
SET embedding_provider_slug = split_part(pmc.embedding_model, '/', 1),
    embedding_model         = substr(pmc.embedding_model, position('/' in pmc.embedding_model) + 1)
FROM kb.project_provider_configs ppc
WHERE ppc.project_id = pmc.project_id
  AND ppc.slug = split_part(pmc.embedding_model, '/', 1)
  AND pmc.embedding_provider_slug IS NULL
  AND pmc.embedding_model LIKE '%/%';

-- 4. Rule (2): a bare or unqualified multi-segment model id that exactly matches
--    one of the project's provider config model columns resolves to that config's
--    slug. Ambiguity (the same model under two distinct slugs) is left unmatched
--    and falls through to rule (3).
UPDATE kb.project_model_config pmc
SET generative_provider_slug = m.slug
FROM (
  SELECT pmc2.project_id, pmc2.generative_model, min(ppc.slug) AS slug
  FROM kb.project_model_config pmc2
  JOIN kb.project_provider_configs ppc
    ON ppc.project_id = pmc2.project_id
   AND ppc.generative_model = pmc2.generative_model
  WHERE pmc2.generative_provider_slug IS NULL
    AND pmc2.generative_model <> ''
  GROUP BY pmc2.project_id, pmc2.generative_model
  HAVING count(DISTINCT ppc.slug) = 1
) m
WHERE m.project_id = pmc.project_id
  AND m.generative_model = pmc.generative_model
  AND pmc.generative_provider_slug IS NULL;

UPDATE kb.project_model_config pmc
SET embedding_provider_slug = m.slug
FROM (
  SELECT pmc2.project_id, pmc2.embedding_model, min(ppc.slug) AS slug
  FROM kb.project_model_config pmc2
  JOIN kb.project_provider_configs ppc
    ON ppc.project_id = pmc2.project_id
   AND ppc.embedding_model = pmc2.embedding_model
  WHERE pmc2.embedding_provider_slug IS NULL
    AND pmc2.embedding_model <> ''
  GROUP BY pmc2.project_id, pmc2.embedding_model
  HAVING count(DISTINCT ppc.slug) = 1
) m
WHERE m.project_id = pmc.project_id
  AND m.embedding_model = pmc.embedding_model
  AND pmc.embedding_provider_slug IS NULL;

-- 5. Rule (3): flag any still-unresolved row (a non-empty model with no slug) for
--    manual resolution instead of silently leaving a NULL slug.
UPDATE kb.project_model_config
SET generative_slug_review_required = true
WHERE generative_provider_slug IS NULL
  AND generative_model <> '';

UPDATE kb.project_model_config
SET embedding_slug_review_required = true
WHERE embedding_provider_slug IS NULL
  AND embedding_model <> '';

-- +goose Down
-- Down drops the structured columns and flags. The bare-name normalization is
-- lossy (a stripped dialect/slug prefix is not restored), matching the provider
-- config backfill's documented one-way behavior.
ALTER TABLE kb.project_model_config DROP COLUMN IF EXISTS embedding_slug_review_required;
ALTER TABLE kb.project_model_config DROP COLUMN IF EXISTS generative_slug_review_required;
ALTER TABLE kb.project_model_config DROP COLUMN IF EXISTS embedding_provider_slug;
ALTER TABLE kb.project_model_config DROP COLUMN IF EXISTS generative_provider_slug;
