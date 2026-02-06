-- +goose Up
-- +goose StatementBegin
-- Make organization_id nullable in document_parsing_jobs
-- This allows document uploads for users without an organization
ALTER TABLE kb.document_parsing_jobs ALTER COLUMN organization_id DROP NOT NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Restore NOT NULL constraint (will fail if there are NULL values)
ALTER TABLE kb.document_parsing_jobs ALTER COLUMN organization_id SET NOT NULL;
-- +goose StatementEnd
