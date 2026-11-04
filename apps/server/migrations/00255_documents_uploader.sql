-- +goose Up
-- +goose StatementBegin
-- Record who uploaded a document (#2260).
--
-- The documents list could say where a document came from but not who brought
-- it in, so the "Uploaded by" column was left out when the list gained filters
-- and stats (#2243). The column is nullable on purpose:
--
--   * documents ingested from a data source have no single uploader — the Source
--     column already answers where they came from;
--   * rows written before this column existed cannot be backfilled, and a
--     guessed attribution is worse than an absent one.
--
-- ON DELETE SET NULL keeps the document when an account is removed: the row is
-- the project's, not the uploader's.
ALTER TABLE kb.documents
    ADD COLUMN IF NOT EXISTS created_by_user_id uuid
        REFERENCES core.user_profiles(id) ON DELETE SET NULL;

COMMENT ON COLUMN kb.documents.created_by_user_id IS
    'Authenticated user who uploaded the document; NULL for source-synced documents and for rows predating #2260.';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE kb.documents DROP COLUMN IF EXISTS created_by_user_id;
-- +goose StatementEnd
