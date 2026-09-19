-- +goose Up
UPDATE kb.graph_objects AS go
SET extraction_job_id = (go.properties->>'_extraction_job_id')::uuid
WHERE go.extraction_job_id IS NULL
  AND go.properties ? '_extraction_job_id'
  AND pg_input_is_valid(go.properties->>'_extraction_job_id', 'uuid')
  AND EXISTS (
    SELECT 1 FROM kb.object_extraction_jobs AS j
    WHERE j.id = (go.properties->>'_extraction_job_id')::uuid
  );

-- +goose Down
-- Data backfill is intentionally not reversible.
