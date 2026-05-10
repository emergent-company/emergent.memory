-- +goose Up
-- +goose StatementBegin
ALTER TABLE kb.graph_objects ADD COLUMN IF NOT EXISTS namespace TEXT;

-- New unique index for namespaced objects on main graph:
-- scoped to (project_id, namespace, type, key), isolated from non-namespaced
CREATE UNIQUE INDEX IF NOT EXISTS idx_graph_objects_upsert_namespace
    ON kb.graph_objects (project_id, namespace, type, key)
    WHERE key IS NOT NULL
      AND namespace IS NOT NULL
      AND supersedes_id IS NULL
      AND deleted_at IS NULL
      AND branch_id IS NULL;

CREATE INDEX IF NOT EXISTS idx_graph_objects_namespace
    ON kb.graph_objects (project_id, namespace)
    WHERE namespace IS NOT NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS kb.idx_graph_objects_upsert_namespace;
DROP INDEX IF EXISTS kb.idx_graph_objects_namespace;
ALTER TABLE kb.graph_objects DROP COLUMN IF EXISTS namespace;
-- +goose StatementEnd
