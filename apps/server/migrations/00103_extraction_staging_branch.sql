-- +goose Up
ALTER TABLE kb.object_extraction_jobs
    ADD COLUMN IF NOT EXISTS staging_branch_id uuid REFERENCES kb.branches(id) ON DELETE SET NULL;

COMMENT ON COLUMN kb.object_extraction_jobs.staging_branch_id IS
    'Staging branch where extracted objects land pending review. NULL = legacy (objects on main) or no branch isolation.';

-- +goose Down
ALTER TABLE kb.object_extraction_jobs DROP COLUMN IF EXISTS staging_branch_id;
