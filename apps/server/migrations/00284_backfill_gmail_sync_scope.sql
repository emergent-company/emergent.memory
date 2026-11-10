-- +goose Up
-- +goose StatementBegin

-- Legacy Gmail sources were created with an implicit in:inbox scope and no
-- persisted filter. The strict scope contract now rejects an empty filter, so
-- backfill the historical default to keep existing sources syncing.
UPDATE kb.sources
SET config = jsonb_set(config, '{filter}', '{"query":"in:inbox"}'::jsonb, true),
    updated_at = now()
WHERE provider = 'gmail'
  AND COALESCE(config->'filter', '{}'::jsonb) = '{}'::jsonb
  AND COALESCE(config->>'query', '') = '';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

UPDATE kb.sources
SET config = config - 'filter',
    updated_at = now()
WHERE provider = 'gmail'
  AND config->'filter' = '{"query":"in:inbox"}'::jsonb;

-- +goose StatementEnd
