-- +goose Up
-- +goose StatementBegin

-- Add ON DELETE CASCADE FK from graph_embedding_jobs.object_id → graph_objects.id
-- Previously there was only an index; orphaned jobs accumulated when objects were deleted.
ALTER TABLE kb.graph_embedding_jobs
    ADD CONSTRAINT fk_graph_embedding_jobs_object_id
    FOREIGN KEY (object_id) REFERENCES kb.graph_objects(id) ON DELETE CASCADE;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE kb.graph_embedding_jobs DROP CONSTRAINT IF EXISTS fk_graph_embedding_jobs_object_id;
-- +goose StatementEnd
