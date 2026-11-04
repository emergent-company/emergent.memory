-- +goose Up
-- Enforce project-scoping on branches: project_id must never be NULL.
--
-- Issue #322: org-global branches (project_id IS NULL) leak into every
-- project's branch list. Backfill migrations 00122/00123 already recovered
-- every NULL branch that had a derivable project (via graph objects, direct
-- children, or branch_lineage descendants). Any remaining NULL rows have no
-- graph objects, no project-scoped children, and no project — they are
-- orphaned bench/e2e/test leakage and are safe to delete.
--
-- Referential safety:
--   * kb.branch_lineage has NO foreign key to kb.branches, so its rows must be
--     cleaned explicitly before deleting branches.
--   * extraction_jobs.staging_branch_id references kb.branches ON DELETE SET
--     NULL — the only FK pointing at kb.branches.
--   * graph_objects.branch_id has no FK; NULL-project branches have no objects.

-- 1. Drop lineage rows referencing orphan branches.
DELETE FROM kb.branch_lineage bl
USING kb.branches b
WHERE b.project_id IS NULL
  AND (bl.branch_id = b.id OR bl.ancestor_branch_id = b.id);

-- 2. Delete the orphan branches themselves.
DELETE FROM kb.branches WHERE project_id IS NULL;

-- 3. Enforce NOT NULL so org-global branches can never be created again.
ALTER TABLE kb.branches ALTER COLUMN project_id SET NOT NULL;

-- +goose Down
ALTER TABLE kb.branches ALTER COLUMN project_id DROP NOT NULL;
-- Deleted orphan rows are intentionally not recoverable.
