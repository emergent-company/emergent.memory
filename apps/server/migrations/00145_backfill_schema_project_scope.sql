-- +goose Up
-- Backfill project_id on orphaned discovered schemas in kb.graph_schemas.
--
-- Root cause: the discovery-jobs auto-discovery write path created
-- source='discovered' schemas without setting project_id. Where the linked
-- discovery job is still resolvable AND its project still exists, we can
-- safely attribute the schema to that project.
--
-- The JOIN to kb.projects intentionally drops any dangling discovery_job_id
-- whose project_id no longer resolves, so those rows are left untouched.
--
-- Rows intentionally left with project_id = NULL:
--   * source='builtin' rows: global builtin schemas are not project-scoped.
--   * source='manual' rows with no discovery_job_id: no project can be derived.
--   * the dangling-project discovered row excluded by the JOIN above.
-- All of these are hidden from every project by the strict project-scoped read
-- filter; ops can still locate them with `WHERE project_id IS NULL`.
UPDATE kb.graph_schemas gs
SET project_id = dj.project_id
FROM kb.discovery_jobs dj
JOIN kb.projects p ON p.id = dj.project_id
WHERE gs.discovery_job_id = dj.id
  AND gs.project_id IS NULL
  AND gs.source IS DISTINCT FROM 'builtin';

-- +goose Down
-- Not safely reversible without a pre-migration snapshot: we cannot distinguish
-- rows that were intentionally NULL from rows this migration scoped.
