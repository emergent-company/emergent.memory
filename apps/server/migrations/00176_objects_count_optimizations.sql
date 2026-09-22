-- +goose Up
-- Issues #732 / #733.
--
-- GET /api/graph/objects/search paid an 18-32 s exact COUNT(*) on the dominant
-- project even though the plan was an "Index Only Scan" over
-- idx_graph_objects_head_main (00163). The scan still touched the heap:
-- kb.graph_objects is a high-churn table and its visibility map was stale, so
-- Heap Fetches was 26329 for ~101k HEAD rows (dev: relallvisible = 67.8% of
-- pages).
--
-- Reproduction on a scratch PostgreSQL 17 database (120k HEAD rows in one
-- project, 00163 partial index, visibility map partially stale):
--   * WITH partial VM:  Parallel Seq Scan 103-119 ms (planner declines the
--     index-only scan); with a narrower (project_id) partial index the planner
--     does pick it but still reports Heap Fetches: 49525 (34 ms).
--   * AFTER VACUUM (ANALYZE): Index Only Scan, Heap Fetches: 0, 41 ms over the
--     existing 00163 index (14 ms over the narrower index).
-- A fresher visibility map is what removes the heap fetches; a narrower partial
-- index alone does not (it only makes the heap-fetch fallback slightly cheaper).
--
-- So the durable fix is maintenance, not schema: make autovacuum vacuum this
-- table far more often than the default (threshold 50 + 20% of live rows). On a
-- table with ~108k live rows the default triggers only after ~21k dead tuples,
-- which is already enough churn to leave most pages without a visibility-map
-- bit. Lowering the scale factor to 2% and the threshold to 500 makes autovacuum
-- run while the map is still mostly fresh, so the index-only count keeps
-- returning in tens of milliseconds.
--
-- These reloptions take effect for the running autovacuum launcher without a
-- restart, and existing bloat is reclaimed by the next autovacuum cycle (it now
-- sees n_dead_tup well above the new threshold). Only per-table storage
-- parameters change: no index is created or dropped, and no table or index is
-- rewritten.

ALTER TABLE kb.graph_objects SET (
  autovacuum_vacuum_scale_factor = 0.02,
  autovacuum_vacuum_threshold = 500,
  autovacuum_analyze_scale_factor = 0.02,
  autovacuum_analyze_threshold = 500
);

-- +goose Down
ALTER TABLE kb.graph_objects RESET (
  autovacuum_vacuum_scale_factor,
  autovacuum_vacuum_threshold,
  autovacuum_analyze_scale_factor,
  autovacuum_analyze_threshold
);
