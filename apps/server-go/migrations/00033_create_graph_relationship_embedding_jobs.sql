-- +goose Up
-- +goose StatementBegin

-- Job queue for async relationship embedding generation.
-- Mirrors kb.graph_embedding_jobs (which handles graph objects) but references kb.graph_relationships.
CREATE TABLE kb.graph_relationship_embedding_jobs (
    id              uuid        NOT NULL DEFAULT gen_random_uuid(),
    relationship_id uuid        NOT NULL REFERENCES kb.graph_relationships(id) ON DELETE CASCADE,
    status          text        NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','processing','completed','failed')),
    priority        integer     NOT NULL DEFAULT 0,
    attempt_count   integer     NOT NULL DEFAULT 0,
    last_error      text,
    scheduled_at    timestamptz NOT NULL DEFAULT now(),
    started_at      timestamptz,
    completed_at    timestamptz,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT graph_relationship_embedding_jobs_pkey PRIMARY KEY (id)
);

CREATE INDEX idx_graph_rel_emb_jobs_relationship_id ON kb.graph_relationship_embedding_jobs (relationship_id);
CREATE INDEX idx_graph_rel_emb_jobs_status           ON kb.graph_relationship_embedding_jobs (status);
CREATE INDEX idx_graph_rel_emb_jobs_scheduled        ON kb.graph_relationship_embedding_jobs (scheduled_at) WHERE status = 'pending';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS kb.graph_relationship_embedding_jobs;
-- +goose StatementEnd
