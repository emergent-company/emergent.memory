-- +goose Up
-- +goose StatementBegin
ALTER TABLE core.api_tokens ADD COLUMN IF NOT EXISTS token_encrypted text;
COMMENT ON COLUMN core.api_tokens.token_encrypted IS 'Encrypted raw token value (pgp_sym_encrypt). Allows retrieval by authorized users.';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE core.api_tokens DROP COLUMN IF EXISTS token_encrypted;
-- +goose StatementEnd
