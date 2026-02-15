-- +goose Up
-- +goose StatementBegin

-- Issue #15: UpdateObject breaks relationship references
--
-- Previously, graph_relationships.src_id and dst_id stored the physical row ID
-- of graph_objects. When an object was updated (new version created via CreateVersion),
-- a new UUID was generated for the row, but relationships still pointed at the old
-- physical ID — orphaning them.
--
-- The fix changes the application to store canonical_id (the stable logical identity)
-- in src_id/dst_id instead. This migration backfills existing relationships that
-- reference old physical IDs to use canonical_ids instead.
--
-- For v1 objects: id == canonical_id, so no change is needed.
-- For v2+ objects: id != canonical_id, so we need to update src_id/dst_id.

-- Backfill src_id: replace physical IDs with canonical_ids where they differ
UPDATE kb.graph_relationships r
SET src_id = go_src.canonical_id
FROM kb.graph_objects go_src
WHERE r.src_id = go_src.id
  AND go_src.id != go_src.canonical_id;

-- Backfill dst_id: replace physical IDs with canonical_ids where they differ
UPDATE kb.graph_relationships r
SET dst_id = go_dst.canonical_id
FROM kb.graph_objects go_dst
WHERE r.dst_id = go_dst.id
  AND go_dst.id != go_dst.canonical_id;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Down migration is not feasible: we cannot reliably restore the original physical IDs
-- since objects may have been updated multiple times. The canonical_id values are
-- equally valid references and the application now expects them.
SELECT 1;
-- +goose StatementEnd
