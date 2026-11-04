-- +goose Up
DROP INDEX IF EXISTS kb.idx_project_journal_branch;

-- +goose Down
CREATE INDEX IF NOT EXISTS idx_project_journal_branch ON kb.project_journal (project_id, branch_id, created_at DESC);
