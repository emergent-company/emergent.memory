-- +goose Up
-- Second pass: backfill project_id on branches that had no graph objects
-- (00122 was a no-op for these rows).
--
-- Pass 1: derive from direct children — if a child branch has project_id set
-- and its parent_branch_id points at an orphan, the parent inherits the project.
UPDATE kb.branches parent
SET project_id = child.project_id
FROM kb.branches child
WHERE child.parent_branch_id = parent.id
  AND child.project_id IS NOT NULL
  AND parent.project_id IS NULL;

-- Pass 2: derive via branch_lineage closure table — find any descendant with a
-- known project_id and attribute it to the ancestor (shortest path wins).
UPDATE kb.branches b
SET project_id = d.project_id
FROM (
    SELECT DISTINCT ON (bl.ancestor_branch_id)
        bl.ancestor_branch_id AS id,
        child.project_id
    FROM kb.branch_lineage bl
    JOIN kb.branches child ON child.id = bl.branch_id
    WHERE child.project_id IS NOT NULL
      AND bl.depth > 0
    ORDER BY bl.ancestor_branch_id, bl.depth ASC
) d
WHERE b.id = d.id
  AND b.project_id IS NULL;

-- +goose Down
-- Not safely reversible.
