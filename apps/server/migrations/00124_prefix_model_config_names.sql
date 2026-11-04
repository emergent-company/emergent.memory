-- +goose Up
-- +goose StatementBegin
-- Prefix bare model names in kb.project_model_config with their provider.
-- Rows inserted before provider-prefix enforcement may have bare names such as
-- "deepseek-v4-flash" or "gemini-embedding-2-preview".  This migration adds the
-- canonical provider prefix so that all rows satisfy the new validation rule.

UPDATE kb.project_model_config
SET generative_model = 'deepseek/' || generative_model
WHERE generative_model NOT LIKE '%/%'
  AND generative_model ILIKE '%deepseek%';

UPDATE kb.project_model_config
SET generative_model = 'google/' || generative_model
WHERE generative_model NOT LIKE '%/%'
  AND generative_model != '';

UPDATE kb.project_model_config
SET embedding_model = 'google/' || embedding_model
WHERE embedding_model NOT LIKE '%/%'
  AND embedding_model != '';

UPDATE kb.org_model_config
SET generative_model = 'deepseek/' || generative_model
WHERE generative_model NOT LIKE '%/%'
  AND generative_model ILIKE '%deepseek%';

UPDATE kb.org_model_config
SET generative_model = 'google/' || generative_model
WHERE generative_model NOT LIKE '%/%'
  AND generative_model != '';

UPDATE kb.org_model_config
SET embedding_model = 'google/' || embedding_model
WHERE embedding_model NOT LIKE '%/%'
  AND embedding_model != '';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Strip provider prefix (reverse — informational only; data may be ambiguous).
UPDATE kb.project_model_config
SET generative_model = SPLIT_PART(generative_model, '/', 2)
WHERE generative_model LIKE '%/%';

UPDATE kb.project_model_config
SET embedding_model = SPLIT_PART(embedding_model, '/', 2)
WHERE embedding_model LIKE '%/%';

UPDATE kb.org_model_config
SET generative_model = SPLIT_PART(generative_model, '/', 2)
WHERE generative_model LIKE '%/%';

UPDATE kb.org_model_config
SET embedding_model = SPLIT_PART(embedding_model, '/', 2)
WHERE embedding_model LIKE '%/%';
-- +goose StatementEnd
