-- +goose Up
-- +goose StatementBegin

-- Legacy Gmail sources were created with an implicit in:inbox scope and no
-- persisted filter. The strict scope contract now rejects an empty filter, so
-- backfill the historical default to keep existing sources syncing.
--
-- Only sources that had no scope at all are rewritten. A label-only source
-- (config.label_ids set, no query, no filter) is a complete scope under the new
-- validator, and stamping in:inbox onto it would narrow "all mail with these
-- labels" to the inbox and drop already-synced archived mail.
UPDATE kb.sources
SET config = jsonb_set(config, '{filter}', '{"query":"in:inbox"}'::jsonb, true),
    updated_at = now()
WHERE provider = 'gmail'
  AND COALESCE(config->'filter', '{}'::jsonb) = '{}'::jsonb
  AND btrim(COALESCE(config->>'query', ''), E' \t\n\r') = ''
  -- jsonb_array_length raises on a non-array, so only count a real array; a
  -- legacy row with a malformed label_ids must not fail the migration.
  AND COALESCE(
        jsonb_array_length(CASE WHEN jsonb_typeof(config->'label_ids') = 'array'
                                THEN config->'label_ids' END), 0) = 0;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

UPDATE kb.sources
SET config = config - 'filter',
    updated_at = now()
WHERE provider = 'gmail'
  AND config->'filter' = '{"query":"in:inbox"}'::jsonb;

-- +goose StatementEnd
