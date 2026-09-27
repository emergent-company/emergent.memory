-- +goose NO TRANSACTION
-- +goose Up
-- Composite GIN index for entity-query property-equality filters (issue #1148).
--
-- entity-query turns its `filters` map into `<property> = <value>` predicates on
-- kb.graph_objects, scoped by project + type + the head-main predicates
-- (deleted_at / supersedes_id / branch_id). There was no index on `properties`,
-- so the pagination COUNT(*) planned as a full Parallel Seq Scan and detoasted
-- the wide `properties` JSONB for every candidate row. On dev (~95k
-- LegalParagraph rows in one project, ~708 MB table) a single filtered page
-- took 80-150 s, which stalled the agent turn until the SSE stream dropped.
--
-- The planner can only use a GIN index when the predicate is expressed as
-- containment (`properties @> '{"key":"value"}'`), so entity-query was rewritten
-- accordingly. The index is composite with btree_gin so project_id and type are
-- index conditions rather than heap recheck filters: a property value that is
-- common across the table (e.g. a repeated `chapter_id`) would otherwise hand
-- the GIN alone a large candidate set.
--
-- Partial on the head-main rows so the index stays small and matches the query
-- predicate exactly. Built CONCURRENTLY (NO TRANSACTION) to avoid an
-- access-exclusive lock on the high-volume graph table; a failed CONCURRENTLY
-- build leaves an INVALID index behind, so drop any leftover first (see 00162).
CREATE EXTENSION IF NOT EXISTS btree_gin;

DROP INDEX CONCURRENTLY IF EXISTS kb.idx_graph_objects_project_type_props_gin;

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_graph_objects_project_type_props_gin
  ON kb.graph_objects USING gin (project_id, type, properties jsonb_path_ops)
  WHERE deleted_at IS NULL AND supersedes_id IS NULL AND branch_id IS NULL;

-- Refresh planner stats so the new index is selected on the property-filter path.
ANALYZE kb.graph_objects;

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS kb.idx_graph_objects_project_type_props_gin;
