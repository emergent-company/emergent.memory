-- +goose Up
ALTER TABLE kb.project_journal ADD COLUMN IF NOT EXISTS branch_id UUID;
ALTER TABLE kb.project_journal_notes ADD COLUMN IF NOT EXISTS branch_id UUID;

CREATE INDEX IF NOT EXISTS idx_project_journal_branch ON kb.project_journal (project_id, branch_id, created_at DESC);

-- +goose Down
DROP INDEX IF EXISTS idx_project_journal_branch;
ALTER TABLE kb.project_journal DROP COLUMN IF EXISTS branch_id;
ALTER TABLE kb.project_journal_notes DROP COLUMN IF EXISTS branch_id;
