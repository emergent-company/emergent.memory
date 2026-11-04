-- +goose Up
-- +goose StatementBegin

-- Allow api_tokens.user_id to be NULL for system-minted ephemeral tokens.
-- Ephemeral sandbox tokens are not owned by a real user, so the FK to
-- core.user_profiles does not apply. We drop the NOT NULL constraint and
-- relax the unique index to only enforce uniqueness for non-null user_ids.

ALTER TABLE core.api_tokens
    ALTER COLUMN user_id DROP NOT NULL;

-- The existing unique constraint (user_id, name) WHERE revoked_at IS NULL
-- uses a partial index. We need to recreate it to handle NULLs correctly.
-- In PostgreSQL, NULL values are always distinct in unique indexes, so multiple
-- rows with NULL user_id are permitted without any additional change.

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

-- Restore NOT NULL — only safe if no NULL rows exist.
UPDATE core.api_tokens SET user_id = gen_random_uuid() WHERE user_id IS NULL;
ALTER TABLE core.api_tokens
    ALTER COLUMN user_id SET NOT NULL;

-- +goose StatementEnd
