-- +goose Up
-- +goose StatementBegin
-- Per-user OAuth credentials for remote MCP servers (per-user-mcp-oauth §1).
--
-- kb.mcp_server_oauth previously held exactly one row per server, keyed by
-- `server_id PRIMARY KEY` — a project-wide credential. This migration makes the
-- credential per-user with a shared fallback:
--
--   * user_id IS NULL     -> the project-wide / shared credential (the fallback
--                            row every tool call resolves when the caller has no
--                            personal credential). Existing connected servers keep
--                            working: their single row migrates in place as this
--                            shared row.
--   * user_id NOT NULL    -> one personal credential per (server, user).
--
-- PostgreSQL cannot put a nullable column in a PRIMARY KEY, so the literal
-- composite PK is replaced by two unique indexes that encode the same invariant:
-- at most one shared row per server and at most one row per (server, user).
-- Existing rows are never orphaned: ADD COLUMN leaves user_id NULL, so they
-- become the shared row exactly as the fallback requires.

ALTER TABLE kb.mcp_server_oauth DROP CONSTRAINT mcp_server_oauth_pkey;

ALTER TABLE kb.mcp_server_oauth
    ADD COLUMN user_id uuid;

-- At most one shared (project-wide) credential per server.
CREATE UNIQUE INDEX mcp_server_oauth_shared_uniq
    ON kb.mcp_server_oauth (server_id) WHERE user_id IS NULL;

-- At most one personal credential per (server, user).
CREATE UNIQUE INDEX mcp_server_oauth_user_uniq
    ON kb.mcp_server_oauth (server_id, user_id) WHERE user_id IS NOT NULL;

-- Lookup of a user's rows across servers.
CREATE INDEX mcp_server_oauth_user_id
    ON kb.mcp_server_oauth (user_id) WHERE user_id IS NOT NULL;

COMMENT ON COLUMN kb.mcp_server_oauth.user_id IS
    'Owning user for a personal OAuth credential; NULL is the project-wide shared credential';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Collapse back to a single project-wide row per server. The shared row (if any)
-- is kept and per-user rows are dropped, matching the pre-migration model.
DELETE FROM kb.mcp_server_oauth WHERE user_id IS NOT NULL;

DROP INDEX IF EXISTS kb.mcp_server_oauth_user_id;
DROP INDEX IF EXISTS kb.mcp_server_oauth_user_uniq;
DROP INDEX IF EXISTS kb.mcp_server_oauth_shared_uniq;

ALTER TABLE kb.mcp_server_oauth DROP COLUMN user_id;

ALTER TABLE kb.mcp_server_oauth ADD PRIMARY KEY (server_id);
-- +goose StatementEnd
