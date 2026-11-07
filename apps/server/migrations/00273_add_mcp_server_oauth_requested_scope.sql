-- +goose Up
-- +goose StatementBegin
-- Operator-configured requested scope for remote MCP servers (mcp-oauth
-- requested-scopes).
--
-- `scope` keeps its existing meaning: the scope set the authorization server
-- granted (or the requested set when the token response omits one). This new
-- column holds the scope set the operator chose on the pre-registered client
-- form, which overrides discovery on every connect.
--
-- Why a separate column: a provider's advertised scopes_supported is a
-- capability list, not an instruction to request everything (RFC 9728 §7.2).
-- The connect flow must request the challenge scope (when present) or the
-- operator's set, and an empty requested set must stay distinguishable from an
-- empty grant.

ALTER TABLE kb.mcp_server_oauth
    ADD COLUMN IF NOT EXISTS requested_scope text NOT NULL DEFAULT '';

COMMENT ON COLUMN kb.mcp_server_oauth.requested_scope IS
    'Operator-configured scope set to request on connect; overrides the challenge scope (RFC 9728 §7.2). Empty means discover.';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE kb.mcp_server_oauth
    DROP COLUMN IF EXISTS requested_scope;
-- +goose StatementEnd
