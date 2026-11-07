-- +goose Up
-- Record why a branch exists, so the UI can label an extraction branch with its
-- source and an operator can filter by it. Visibility (who may read a branch)
-- is deliberately NOT modelled here: every branch is read the same way, and
-- origin is metadata for labels, filters and cleanup.
--
--   extraction — a per-job staging branch written by the extraction worker
--   agent      — created by an agent run (graph-branch-create inside a run)
--   manual     — created by a person through the API or the UI
--   system     — created by a scheduled or blueprint-driven process
--
-- origin_id is polymorphic (extraction job id, agent run id, user id): there is
-- exactly one creator per branch and nothing joins on it. origin_label is
-- denormalised so a branch list renders without a join — the source document
-- title, the agent name, or the user's name.
ALTER TABLE kb.branches
    ADD COLUMN IF NOT EXISTS origin       text NOT NULL DEFAULT 'manual',
    ADD COLUMN IF NOT EXISTS origin_id    uuid,
    ADD COLUMN IF NOT EXISTS origin_label text;

ALTER TABLE kb.branches
    DROP CONSTRAINT IF EXISTS branches_origin_check;
ALTER TABLE kb.branches
    ADD CONSTRAINT branches_origin_check
    CHECK (origin IN ('extraction', 'agent', 'manual', 'system'));

-- Backfill the extraction branches that already exist. The job link is the
-- authoritative record — the name prefix (extraction/<doc>/<job>) is cosmetic
-- and a branch can be renamed through graph-branch-update.
UPDATE kb.branches b
   SET origin     = 'extraction',
       origin_id  = j.id,
       origin_label = COALESCE(
           (SELECT NULLIF(TRIM(d.filename), '') FROM kb.documents d WHERE d.id = j.document_id),
           b.name)
  FROM kb.object_extraction_jobs j
 WHERE j.staging_branch_id = b.id
   AND b.origin = 'manual';

-- "Left for review" (issue #242) and pending-branch listing both read this.
CREATE INDEX IF NOT EXISTS idx_branches_project_pending
    ON kb.branches (project_id, created_at DESC)
    WHERE merged_at IS NULL;

-- +goose Down
DROP INDEX IF EXISTS kb.idx_branches_project_pending;

ALTER TABLE kb.branches DROP CONSTRAINT IF EXISTS branches_origin_check;
ALTER TABLE kb.branches
    DROP COLUMN IF EXISTS origin_label,
    DROP COLUMN IF EXISTS origin_id,
    DROP COLUMN IF EXISTS origin;
