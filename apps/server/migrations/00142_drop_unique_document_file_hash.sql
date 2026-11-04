-- +goose Up
-- +goose StatementBegin

-- Uploads are no longer deduplicated by file hash (issue #381): each upload
-- creates a new document. The unique index (added in 00041) would reject a
-- second upload of byte-identical content with a unique-violation, so drop it.
DROP INDEX IF EXISTS kb.idx_documents_project_file_hash;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

CREATE UNIQUE INDEX idx_documents_project_file_hash
    ON kb.documents (project_id, file_hash)
    WHERE file_hash IS NOT NULL;

-- +goose StatementEnd
