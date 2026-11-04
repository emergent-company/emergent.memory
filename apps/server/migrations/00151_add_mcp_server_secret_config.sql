-- +goose Up
-- +goose StatementBegin
-- Adds encrypted-at-rest secret storage for MCP server env vars and HTTP headers.
--
-- Secret values are supplied as plaintext in the normal env/headers maps on write
-- and moved by the service into these columns as AES-GCM ciphertext (keyed by
-- LLM_ENCRYPTION_KEY). The API never returns these columns; responses expose
-- only the key names via secretEnvKeys/secretHeadersKeys.
ALTER TABLE kb.mcp_servers ADD COLUMN IF NOT EXISTS secret_env JSONB DEFAULT '{}';
ALTER TABLE kb.mcp_servers ADD COLUMN IF NOT EXISTS secret_headers JSONB DEFAULT '{}';

COMMENT ON COLUMN kb.mcp_servers.secret_env IS 'AES-GCM encrypted secret env vars: {KEY: {ct, nonce}}';
COMMENT ON COLUMN kb.mcp_servers.secret_headers IS 'AES-GCM encrypted secret HTTP headers: {KEY: {ct, nonce}}';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE kb.mcp_servers DROP COLUMN IF EXISTS secret_env;
ALTER TABLE kb.mcp_servers DROP COLUMN IF EXISTS secret_headers;
-- +goose StatementEnd
