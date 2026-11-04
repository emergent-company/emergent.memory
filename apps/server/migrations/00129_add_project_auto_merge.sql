-- +goose Up
-- Per-project flag: when true, object-extraction staging branches are
-- auto-merged into the main graph after extraction completes (partial merge:
-- clean additions are applied, conflicting/similar objects are left for review).
ALTER TABLE kb.projects ADD COLUMN auto_merge_extraction_branches boolean NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE kb.projects DROP COLUMN auto_merge_extraction_branches;
