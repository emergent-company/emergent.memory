-- +goose Up
-- +goose StatementBegin

-- Add partial unique index to enforce uniqueness of (project_id, type, key) for HEAD versions.
-- This supports the upsert/CreateOrUpdate feature: only one live HEAD version per (type, key) 
-- combination is allowed within a project+branch scope.
-- The existing IDX_graph_objects_key is a non-unique btree index; this adds uniqueness enforcement.

-- For objects on the main branch (branch_id IS NULL)
CREATE UNIQUE INDEX IF NOT EXISTS "IDX_graph_objects_upsert_main"
    ON kb.graph_objects (project_id, type, key)
    WHERE key IS NOT NULL AND supersedes_id IS NULL AND deleted_at IS NULL AND branch_id IS NULL;

-- For objects on a specific branch (branch_id IS NOT NULL)
CREATE UNIQUE INDEX IF NOT EXISTS "IDX_graph_objects_upsert_branch"
    ON kb.graph_objects (project_id, branch_id, type, key)
    WHERE key IS NOT NULL AND supersedes_id IS NULL AND deleted_at IS NULL AND branch_id IS NOT NULL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS kb."IDX_graph_objects_upsert_main";
DROP INDEX IF EXISTS kb."IDX_graph_objects_upsert_branch";
-- +goose StatementEnd
