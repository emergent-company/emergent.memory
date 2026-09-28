-- +goose NO TRANSACTION
-- +goose Up
-- Partial bytewise btree index for entity-query's key_prefix scope (issue #1191).
--
-- entity-query filters entities with starts_with(go.key, ?). starts_with() is a
-- function call, so the planner cannot convert it into a btree range; on an
-- en_US.utf8 database (dev) the prefix filter forced a Parallel Seq Scan of the
-- whole kb.graph_objects heap on every call (~95k LegalParagraph rows in one
-- project: ~20.5s per call, constant regardless of the rows actually returned).
--
-- The existing unique index IDX_graph_objects_upsert_main (project_id, type,
-- key) uses the database default collation, whose ordering is not bytewise
-- (glibc ignores punctuation, so a trailing-separator prefix cannot be bounded
-- by incrementing its last byte and comparing with >= / <). A COLLATE "C"
-- (bytewise) index lets entity-query express the prefix as an explicit
-- [prefix, prefix+1) byte range that the planner serves as an Index Scan.
--
-- Partial on the head-main rows so it matches the query predicate exactly and
-- stays small. Built CONCURRENTLY (NO TRANSACTION) to avoid an access-exclusive
-- lock on the high-volume graph table; a failed CONCURRENTLY build leaves an
-- INVALID index behind, so drop any leftover first (see 00162 for the same
-- rationale).
DROP INDEX CONCURRENTLY IF EXISTS kb.idx_graph_objects_project_type_key_c;

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_graph_objects_project_type_key_c
  ON kb.graph_objects (project_id, type, key COLLATE "C")
  WHERE deleted_at IS NULL AND supersedes_id IS NULL AND branch_id IS NULL;

-- Refresh planner stats so the new index is selected on the key_prefix path.
ANALYZE kb.graph_objects;

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS kb.idx_graph_objects_project_type_key_c;
