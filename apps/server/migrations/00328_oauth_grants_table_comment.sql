-- +goose Up
-- +goose StatementBegin

-- 00254 described a grant as only "the project-scoped API token minted for it",
-- which stopped being the whole truth once a grant also records its consent
-- model (#1163, `project_scope`, added by 00327) and an account-scoped grant may
-- switch the MCP session to any project the owning user belongs to.
--
-- 00254 is immutable (scripts/preflight/migration-immutability.sh), so the
-- comment is restated here instead of edited in place. docs/schema.dbml renders
-- this comment as the table's note and is regenerated with this migration.
COMMENT ON TABLE core.oauth_grants IS 'Links a registered OAuth client to the project API token minted for it; project_id records the starting project and project_scope the consent model: project (bound to that project) or account (may switch to any project the owning user belongs to)';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

-- 00254's original text; the column added by 00327 has its own comment.
COMMENT ON TABLE core.oauth_grants IS 'Links a registered OAuth client to the project-scoped API token minted for it';

-- +goose StatementEnd
