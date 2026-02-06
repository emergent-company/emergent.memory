-- +goose Up
-- +goose StatementBegin
CREATE UNIQUE INDEX IF NOT EXISTS idx_graph_object_revision_counts_unique 
ON kb.graph_object_revision_counts(canonical_id, project_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS kb.idx_graph_object_revision_counts_unique;
-- +goose StatementEnd
