-- +goose Up
ALTER TABLE core.api_tokens ADD COLUMN IF NOT EXISTS expires_at TIMESTAMPTZ NULL;

CREATE INDEX IF NOT EXISTS idx_api_tokens_expires_at
    ON core.api_tokens (expires_at)
    WHERE expires_at IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS core.idx_api_tokens_expires_at;
ALTER TABLE core.api_tokens DROP COLUMN IF EXISTS expires_at;
