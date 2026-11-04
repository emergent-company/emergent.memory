-- +goose Up
-- Backfill project_id on kb.branches rows where it is NULL.
-- Derives the project from the oldest graph object written to that branch.
-- Branches that cannot be matched (no objects ever written) are left as-is.
UPDATE kb.branches b
SET project_id = o.project_id
FROM (
    SELECT DISTINCT ON (branch_id) branch_id, project_id
    FROM kb.graph_objects
    WHERE branch_id IS NOT NULL
    ORDER BY branch_id, created_at ASC
) o
WHERE b.id::text = o.branch_id::text
  AND b.project_id IS NULL;

-- +goose Down
-- Not safely reversible: we do not know which rows were intentionally NULL.
-- A targeted rollback would require a pre-migration snapshot.
