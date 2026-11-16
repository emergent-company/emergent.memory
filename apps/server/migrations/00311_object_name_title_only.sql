-- +goose NO TRANSACTION
-- +goose Up
--
-- #824: `kb.graph_objects.name` is the single title carrier.
--
-- 00280 added the column but backfilled it from the properties convention and
-- then from `key`, and its machine-key guard only recognised the current
-- generated shape (`<type>-<12 hex>`). Two classes of junk title survived:
--
--   * legacy generated keys, `<type>-<8 hex>` (the old autoWorkKey output);
--   * source-scoped dedup keys, `<provider>:<uuid>:<external id>`.
--
-- Both were copied into `name`, so the display layer renders an identity string
-- as a title. The write and read paths no longer fall back to
-- properties.name/title (resolveObjectName), so repairing the data is all that
-- is left here.
--
-- Repair: clear a `name` that is a verbatim copy of a machine-shaped `key`.
-- Backfill: title a still-nameless object from a key that is a real human
-- identifier. A machine key stays NULL, and the object renders as
-- `type · #ref` in the UI instead of showing its identity.
--
-- Machine forms (the same three the SQL predicate and the UI regex recognise):
--   ^[a-z0-9_-]+-[0-9a-f]{12}$                                          current
--   ^[a-z0-9_-]+-[0-9a-f]{8}$                                           legacy
--   ^[a-z][a-z0-9_-]*:[0-9a-f]{8}-<uuid tail>:<external id>             source
--
-- The legacy 8-hex form is ambiguous with a human key that ends in 8 hex
-- characters (a date suffix, e.g. `report-20251116`). It is treated as machine
-- on purpose: such an object is left nameless and renders as `type · #ref`
-- instead of risking an identity string shown as a title.
--
-- `name` is not NOT NULL: a nameless write is accepted (a provider record or an
-- extraction result legitimately has no title), so there is no constraint to
-- add. The update trigger (00280) rebuilds `fts` when `name` changes, so the
-- UPDATEs below re-index the rows they touch; no explicit FTS rebuild is needed.
--
-- NO TRANSACTION because the index replacement is built CONCURRENTLY. Each
-- statement commits on its own, so a failed run leaves the repair applied and
-- the version unrecorded; both UPDATEs are idempotent and the index is
-- drop-then-create, so a retry completes cleanly.

-- ---- 1. Repair: a machine key is never a title ----------------------------
UPDATE kb.graph_objects
   SET name = NULL
 WHERE name IS NOT NULL
   AND key IS NOT NULL
   AND name = key
   AND (
        key ~ '^[a-z0-9_-]+-[0-9a-f]{12}$'
     OR key ~ '^[a-z0-9_-]+-[0-9a-f]{8}$'
     OR key ~ '^[a-z][a-z0-9_-]*:[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}:.+$'
   );

-- ---- 2. Backfill: a human key is a usable title ---------------------------
UPDATE kb.graph_objects
   SET name = key
 WHERE key IS NOT NULL
   AND key <> ''
   AND (name IS NULL OR btrim(name) = '')
   AND NOT (
        key ~ '^[a-z0-9_-]+-[0-9a-f]{12}$'
     OR key ~ '^[a-z0-9_-]+-[0-9a-f]{8}$'
     OR key ~ '^[a-z][a-z0-9_-]*:[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}:.+$'
   );

-- ---- 3. sort_by=name orders by the title column ---------------------------
-- entity-query's `sort_by=name` now orders by `left(go.name, 256)` (the
-- properties->>'name' fallback is gone, #824). Replace the 00199 expression
-- index so the ordering stays an ordered index scan with no Sort node. The
-- 256-char bound is load-bearing: `name` is client-supplied and unbounded, and
-- indexing it raw can exceed the ~2704-byte btree tuple limit (SQLSTATE 54000).
-- Partial on the head-main predicate entity-query always uses. Built
-- CONCURRENTLY; drop the target name first so an INVALID leftover is rebuilt.
DROP INDEX CONCURRENTLY IF EXISTS kb.idx_graph_objects_project_type_title_sort;

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_graph_objects_project_type_title_sort
  ON kb.graph_objects (project_id, type, left(name, 256) DESC NULLS LAST, key)
  WHERE deleted_at IS NULL AND supersedes_id IS NULL AND branch_id IS NULL;

DROP INDEX CONCURRENTLY IF EXISTS kb.idx_graph_objects_project_type_name;

-- `idx_graph_objects_project_type_name_lower` (00280) is an unbounded
-- expression index on `name`, which is now the write target for every title.
-- `name` is client-supplied (the API caps it at 512, the column does not), so a
-- longer direct write fails on this index: an incompressible 20 KB name needs a
-- 20 KB index tuple against an ~8 KB btree limit (SQLSTATE 54000). No query
-- reads the index — nothing filters on lower(name) — so it is dropped rather
-- than re-bounded; the 256-char sort index above is the one entity-query uses.
DROP INDEX CONCURRENTLY IF EXISTS kb.idx_graph_objects_project_type_name_lower;

-- Post-condition guard: with NO TRANSACTION every statement commits on its own,
-- so a build that did not take effect could otherwise be recorded as applied
-- while the old index is gone. Raise and let goose refuse the version; the
-- drop-then-create above makes the retry safe.
-- StatementBegin/End is required because goose's NO TRANSACTION splitter does
-- not understand dollar-quoted bodies.
-- +goose StatementBegin
DO $$
DECLARE
    ok boolean;
BEGIN
    SELECT EXISTS (
        SELECT 1
        FROM pg_index i
        JOIN pg_class c ON c.oid = i.indexrelid
        JOIN pg_namespace n ON n.oid = c.relnamespace
        WHERE n.nspname = 'kb'
          AND c.relname = 'idx_graph_objects_project_type_title_sort'
          AND i.indisvalid
          AND i.indisready
    ) INTO ok;
    IF NOT ok THEN
        RAISE EXCEPTION '00311: title sort index is missing or invalid';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM pg_class c
        JOIN pg_namespace n ON n.oid = c.relnamespace
        WHERE n.nspname = 'kb'
          AND c.relname = 'idx_graph_objects_project_type_name'
    ) THEN
        RAISE EXCEPTION '00311: properties-name sort index still present after migration';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM pg_class c
        JOIN pg_namespace n ON n.oid = c.relnamespace
        WHERE n.nspname = 'kb'
          AND c.relname = 'idx_graph_objects_project_type_name_lower'
    ) THEN
        RAISE EXCEPTION '00311: unbounded lower(name) index still present after migration';
    END IF;
END
$$;
-- +goose StatementEnd

-- Refresh planner stats so the new index is chosen on the entity-query path.
ANALYZE kb.graph_objects;

-- +goose Down
-- Restore the 00199 properties-based sort index. The data steps are NOT
-- reversed: the rows step 1 cleared held a copy of their own machine key (still
-- in `key`), and step 2's backfill is reproducible from `key` at any time, so a
-- down migration that re-polluted `name` would contradict the invariant this
-- migration establishes. Only the index is reverted.
DROP INDEX CONCURRENTLY IF EXISTS kb.idx_graph_objects_project_type_title_sort;

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_graph_objects_project_type_name
  ON kb.graph_objects (project_id, type, left(properties->>'name', 256) DESC NULLS LAST, key)
  WHERE deleted_at IS NULL AND supersedes_id IS NULL AND branch_id IS NULL;

-- Restore the 00280 name-lower index verbatim, so a down/up round trip leaves
-- the pre-00311 schema. It has no reader (see the Up comment); if a row with an
-- over-long `name` was written while 00311 was applied this recreation can fail
-- with SQLSTATE 54000, which is the hazard the Up migration removes.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_graph_objects_project_type_name_lower
  ON kb.graph_objects (project_id, type, lower(name));
