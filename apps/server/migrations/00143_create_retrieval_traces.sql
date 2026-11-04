-- +goose Up
-- +goose StatementBegin
-- Retrieval trace persistence for unified search. Each search execution writes
-- one row (query, applied filters, capped candidate list, ordered selected IDs
-- and scores) so a trace ID returned by search can be reconstructed later.
CREATE TABLE IF NOT EXISTS kb.retrieval_traces (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id    UUID NOT NULL REFERENCES kb.projects(id) ON DELETE CASCADE,
    trace_id      UUID NOT NULL,
    query         TEXT NOT NULL,
    filters       JSONB,
    candidates    JSONB,
    selected_ids  JSONB,
    scores        JSONB,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_retrieval_traces_project ON kb.retrieval_traces(project_id);
CREATE INDEX IF NOT EXISTS idx_retrieval_traces_trace ON kb.retrieval_traces(trace_id);
CREATE INDEX IF NOT EXISTS idx_retrieval_traces_created ON kb.retrieval_traces(created_at);

COMMENT ON TABLE kb.retrieval_traces IS 'Persisted retrieval traces for unified search debugging and reconstruction';
COMMENT ON COLUMN kb.retrieval_traces.filters IS 'Search filters applied: resultTypes, fusionStrategy, weights, minScore';
COMMENT ON COLUMN kb.retrieval_traces.candidates IS 'Capped candidate list (id, type, score) fed into fusion';
COMMENT ON COLUMN kb.retrieval_traces.selected_ids IS 'Ordered IDs of the fused results returned to the caller';
COMMENT ON COLUMN kb.retrieval_traces.scores IS 'Fused scores of selected_ids, in the same order';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS kb.retrieval_traces;
-- +goose StatementEnd
