-- +goose Up
-- +goose StatementBegin
-- Refresh the kb.mcp_servers.required_config column comment: registry-installed
-- servers now also persist the registry's own guidance for each key
-- (description + header value template) so the configuration UI can show what
-- to enter. The JSONB shape is additive — existing rows keep working.
COMMENT ON COLUMN kb.mcp_servers.required_config IS 'credential keys (kind=header|env) the server needs before it can be enabled; [{"name","kind","isSecret","isRequired","description","valueHint"}]';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
COMMENT ON COLUMN kb.mcp_servers.required_config IS 'credential keys (kind=header|env) the server needs before it can be enabled; [{"name","kind","isSecret","isRequired"}]';
-- +goose StatementEnd
