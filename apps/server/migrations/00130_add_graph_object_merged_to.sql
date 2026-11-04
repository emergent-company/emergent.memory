-- +goose Up
-- Merge ledger: when a source-branch object is cloned to a target branch during
-- a merge, the source row records the target canonical_id. A later compare/diff
-- uses this to recognize the object as "already merged" instead of "added".
ALTER TABLE kb.graph_objects ADD COLUMN merged_to_canonical_id uuid;

-- +goose Down
ALTER TABLE kb.graph_objects DROP COLUMN merged_to_canonical_id;
