-- +goose Up
-- +goose StatementBegin

-- Before discovery reconciled in place, every run inserted a new
-- "Discovered Types - <timestamp>" row with pending_review = true and
-- superseded_by = NULL. Production Huma KB accumulated 19 such rows for one
-- project (issue #497). Keep the newest pending discovered pack per project and
-- mark the older appends superseded; the kept row is updated in place by the
-- next discovery run instead of another row being appended.
--
-- This migration only touches kb.graph_schemas metadata. It does NOT rename,
-- merge or delete any kb.graph_objects rows: the graph itself is untouched and
-- existing object types keep their names.
WITH ranked AS (
    SELECT id,
           project_id,
           first_value(id) OVER (
               PARTITION BY project_id
               ORDER BY created_at DESC, id DESC
           ) AS keeper_id,
           row_number() OVER (
               PARTITION BY project_id
               ORDER BY created_at DESC, id DESC
           ) AS rn
    FROM kb.graph_schemas
    WHERE source = 'discovered'
      AND pending_review = true
      AND superseded_by IS NULL
      AND project_id IS NOT NULL
)
UPDATE kb.graph_schemas AS gs
SET superseded_by = ranked.keeper_id::text,
    pending_review = false,
    updated_at = now()
FROM ranked
WHERE gs.id = ranked.id
  AND ranked.rn > 1;

-- Enforce the invariant at the database boundary: one current pending
-- discovered pack per project. Finalised/reviewed packs are pending_review =
-- false and are not covered by the partial index.
CREATE UNIQUE INDEX IF NOT EXISTS uq_graph_schemas_current_discovered
    ON kb.graph_schemas (project_id)
    WHERE source = 'discovered'
      AND pending_review = true
      AND superseded_by IS NULL
      AND project_id IS NOT NULL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP INDEX IF EXISTS kb.uq_graph_schemas_current_discovered;

-- Best-effort restoration of the pre-migration shape. The migration cannot
-- distinguish rows it superseded from rows superseded by later code, so only
-- rows tagged by this backfill (still source='discovered', pending_review=false,
-- superseded_by set) are reopened.
UPDATE kb.graph_schemas
SET pending_review = true,
    superseded_by = NULL,
    updated_at = now()
WHERE source = 'discovered'
  AND pending_review = false
  AND superseded_by IS NOT NULL;

-- +goose StatementEnd
