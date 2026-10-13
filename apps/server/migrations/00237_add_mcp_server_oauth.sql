-- +goose Up
-- +goose StatementBegin
-- OAuth credentials for remote MCP servers (add-mcp-oauth-connect §1).
--
-- One row per MCP server that has been authorized (or has a registration in
-- flight) against an OAuth authorization server. The row holds the discovered
-- endpoint set, the dynamically registered client identity, and the token set.
-- Every secret value (client secret, access token, refresh token) is stored as
-- AES-GCM ciphertext + nonce in the {ct, nonce} shape used by
-- kb.mcp_servers.secret_headers, so no token material is readable at rest and
-- none of it is ever serialized into a DTO.

CREATE TABLE IF NOT EXISTS kb.mcp_server_oauth (
    server_id             uuid PRIMARY KEY REFERENCES kb.mcp_servers(id) ON DELETE CASCADE,
    project_id            uuid NOT NULL,
    authorization_server  text NOT NULL DEFAULT '',
    token_endpoint        text NOT NULL DEFAULT '',
    registration_endpoint text NOT NULL DEFAULT '',
    resource              text NOT NULL DEFAULT '',
    scope                 text NOT NULL DEFAULT '',
    client_id             text NOT NULL DEFAULT '',
    client_secret         jsonb,
    access_token          jsonb,
    refresh_token         jsonb,
    expires_at            timestamptz,
    created_at            timestamptz NOT NULL DEFAULT now(),
    updated_at            timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_mcp_server_oauth_project_id
    ON kb.mcp_server_oauth (project_id);

COMMENT ON TABLE kb.mcp_server_oauth IS 'OAuth credentials (client registration + token set) for a remote MCP server';
COMMENT ON COLUMN kb.mcp_server_oauth.resource IS 'RFC 8707 protected resource identifier sent on authorize/token/refresh';
COMMENT ON COLUMN kb.mcp_server_oauth.client_secret IS 'AES-GCM encrypted client secret: {ct, nonce}';
COMMENT ON COLUMN kb.mcp_server_oauth.access_token IS 'AES-GCM encrypted access token: {ct, nonce}';
COMMENT ON COLUMN kb.mcp_server_oauth.refresh_token IS 'AES-GCM encrypted refresh token: {ct, nonce}';

ALTER TABLE kb.mcp_server_oauth ENABLE ROW LEVEL SECURITY;

CREATE POLICY mcp_server_oauth_select ON kb.mcp_server_oauth FOR SELECT USING (
    (COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text)
    OR ((project_id)::text = current_setting('app.current_project_id'::text, true))
);

CREATE POLICY mcp_server_oauth_insert ON kb.mcp_server_oauth FOR INSERT WITH CHECK (
    (COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text)
    OR ((project_id)::text = current_setting('app.current_project_id'::text, true))
);

CREATE POLICY mcp_server_oauth_update ON kb.mcp_server_oauth FOR UPDATE USING (
    (COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text)
    OR ((project_id)::text = current_setting('app.current_project_id'::text, true))
) WITH CHECK (
    (COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text)
    OR ((project_id)::text = current_setting('app.current_project_id'::text, true))
);

CREATE POLICY mcp_server_oauth_delete ON kb.mcp_server_oauth FOR DELETE USING (
    (COALESCE(current_setting('app.current_project_id'::text, true), ''::text) = ''::text)
    OR ((project_id)::text = current_setting('app.current_project_id'::text, true))
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS kb.mcp_server_oauth;
-- +goose StatementEnd
