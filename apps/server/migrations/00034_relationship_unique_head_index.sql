-- +goose Up
-- +goose StatementBegin

-- Partial unique index for HEAD relationships on the main branch (branch_id IS NULL).
-- Enforces at most one live HEAD per (project, type, src, dst) on main branch.
-- This enables lock-free INSERT ... ON CONFLICT DO NOTHING instead of advisory locks.
CREATE UNIQUE INDEX IF NOT EXISTS uq_graph_relationships_head_main
    ON kb.graph_relationships (project_id, type, src_id, dst_id)
    WHERE supersedes_id IS NULL AND branch_id IS NULL;

-- Partial unique index for HEAD relationships on named branches (branch_id IS NOT NULL).
CREATE UNIQUE INDEX IF NOT EXISTS uq_graph_relationships_head_branch
    ON kb.graph_relationships (project_id, branch_id, type, src_id, dst_id)
    WHERE supersedes_id IS NULL AND branch_id IS NOT NULL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS kb.uq_graph_relationships_head_main;
DROP INDEX IF EXISTS kb.uq_graph_relationships_head_branch;
-- +goose StatementEnd
