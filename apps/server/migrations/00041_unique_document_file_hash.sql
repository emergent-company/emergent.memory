-- +goose Up
-- +goose StatementBegin

-- Make file_hash deduplication index unique so concurrent uploads of the same
-- file cannot produce duplicate rows (pairs well with the TOCTOU-safe retry
-- logic in CreateFromUpload).
DROP INDEX IF EXISTS kb.idx_documents_project_file_hash;
CREATE UNIQUE INDEX idx_documents_project_file_hash
    ON kb.documents (project_id, file_hash)
    WHERE file_hash IS NOT NULL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP INDEX IF EXISTS kb.idx_documents_project_file_hash;
CREATE INDEX idx_documents_project_file_hash
    ON kb.documents (project_id, file_hash)
    WHERE file_hash IS NOT NULL;

-- +goose StatementEnd
