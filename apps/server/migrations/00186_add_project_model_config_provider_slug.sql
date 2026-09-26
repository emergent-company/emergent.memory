-- +goose Up
-- Record the provider instance slug for each default model, making the
-- selection structured (provider + model). The model columns keep holding the
-- routed name; the slug column carries the instance identity. Unqualified
-- multi-segment model ids (Vertex resource paths) keep a NULL slug.
ALTER TABLE kb.project_model_config
  ADD COLUMN IF NOT EXISTS generative_provider_slug varchar(63);
ALTER TABLE kb.project_model_config
  ADD COLUMN IF NOT EXISTS embedding_provider_slug varchar(63);

UPDATE kb.project_model_config
SET generative_provider_slug = split_part(generative_model, '/', 1)
WHERE generative_model LIKE '%/%'
  AND split_part(generative_model, '/', 1) IN ('google', 'google-vertex', 'openai', 'deepseek');

UPDATE kb.project_model_config
SET embedding_provider_slug = split_part(embedding_model, '/', 1)
WHERE embedding_model LIKE '%/%'
  AND split_part(embedding_model, '/', 1) IN ('google', 'google-vertex', 'openai', 'deepseek');

-- +goose Down
ALTER TABLE kb.project_model_config DROP COLUMN IF EXISTS embedding_provider_slug;
ALTER TABLE kb.project_model_config DROP COLUMN IF EXISTS generative_provider_slug;
