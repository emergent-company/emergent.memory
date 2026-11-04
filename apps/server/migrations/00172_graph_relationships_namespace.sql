-- +goose Up
-- +goose StatementBegin
-- Denormalise namespace from the source object onto the relationship so the
-- relationship ANN predicate can be applied to the table that owns the
-- embedding (kb.graph_relationships), mirroring project_id. Applying the
-- namespace filter on the joined kb.graph_objects row prevents pgvector from
-- satisfying it with the embedding index and forces a sequential scan over
-- every embedded relationship.
ALTER TABLE kb.graph_relationships ADD COLUMN IF NOT EXISTS namespace TEXT;

CREATE INDEX IF NOT EXISTS idx_graph_relationships_namespace
    ON kb.graph_relationships (project_id, namespace)
    WHERE namespace IS NOT NULL;

-- Backfill from the source object. src_id stores the source object's
-- canonical_id, which is the v1 row's physical id, so src.id = gr.src_id
-- resolves the namespace-bearing row (the same join the search query used).
UPDATE kb.graph_relationships gr
SET namespace = src.namespace
FROM kb.graph_objects src
WHERE src.id = gr.src_id
  AND gr.namespace IS NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS kb.idx_graph_relationships_namespace;
ALTER TABLE kb.graph_relationships DROP COLUMN IF EXISTS namespace;
-- +goose StatementEnd
