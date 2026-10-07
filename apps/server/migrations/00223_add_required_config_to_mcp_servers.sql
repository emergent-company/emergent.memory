-- +goose Up
-- +goose StatementBegin
-- Adds per-server required-configuration metadata to kb.mcp_servers.
--
-- `required_config` records the credential keys (header or env) a server needs
-- before it can be enabled. It is populated on registry install from the
-- selected transport's declaration (headers/env vars marked isRequired or
-- isSecret); manual creates default to an empty list. The enable gate in
-- Service.UpdateServer rejects enabling a server while a required key is blank.
ALTER TABLE kb.mcp_servers ADD COLUMN IF NOT EXISTS required_config jsonb NOT NULL DEFAULT '[]';

COMMENT ON COLUMN kb.mcp_servers.required_config IS 'credential keys (kind=header|env) the server needs before it can be enabled; [{"name","kind","isSecret","isRequired"}]';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE kb.mcp_servers DROP COLUMN IF EXISTS required_config;
-- +goose StatementEnd
