-- +goose Up
-- +goose StatementBegin

-- Make project_id nullable (account tokens have no project)
ALTER TABLE core.api_tokens ALTER COLUMN project_id DROP NOT NULL;

-- Drop the old per-project uniqueness constraint
ALTER TABLE core.api_tokens DROP CONSTRAINT IF EXISTS api_tokens_project_name_unique;

-- Deduplicate any existing active tokens with the same (user_id, name) pair
-- (keep the most recently created one, revoke the rest)
UPDATE core.api_tokens
SET revoked_at = NOW()
WHERE id IN (
    SELECT id FROM (
        SELECT id,
               ROW_NUMBER() OVER (PARTITION BY user_id, name ORDER BY created_at DESC) AS rn
        FROM core.api_tokens
        WHERE revoked_at IS NULL
    ) ranked
    WHERE rn > 1
);

-- New: uniqueness per user (active tokens only; revoked tokens may reuse names)
CREATE UNIQUE INDEX api_tokens_user_name_unique
    ON core.api_tokens (user_id, name)
    WHERE revoked_at IS NULL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP INDEX IF EXISTS core.api_tokens_user_name_unique;

ALTER TABLE core.api_tokens ADD CONSTRAINT api_tokens_project_name_unique UNIQUE (project_id, name);

-- Note: restoring NOT NULL would fail if any account tokens were created; clean those first.
-- ALTER TABLE core.api_tokens ALTER COLUMN project_id SET NOT NULL;

-- +goose StatementEnd
