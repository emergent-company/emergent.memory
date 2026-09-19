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

CREATE INDEX IF NOT EXISTS idx_graph_objects_project_extraction_job
  ON kb.graph_objects (project_id, extraction_job_id);

-- +goose Down
DROP INDEX IF EXISTS kb.idx_graph_objects_project_extraction_job;
-- Data backfill is intentionally not reversible.
