-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS kb.embedding_cache (
    cache_key  TEXT        NOT NULL PRIMARY KEY,
    model_id   TEXT        NOT NULL DEFAULT '',
    embedding  JSONB       NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS kb.embedding_cache;
-- +goose StatementEnd
