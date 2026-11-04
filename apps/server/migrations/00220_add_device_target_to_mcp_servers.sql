-- +goose Up
-- +goose StatementBegin
-- Adds server-authoritative device MCP configuration targeting to kb.mcp_servers.
--
-- `target` distinguishes server-connected registry rows ('server', the default)
-- from rows pushed to a connected relay device ('device'). A device-targeted row
-- is never connected outbound by the server; it is delivered over the relay.
-- `target_instance_id` is NULL for a broadcast row (every eligible device of the
-- project) or the relay instance_id of a specific device.
ALTER TABLE kb.mcp_servers ADD COLUMN IF NOT EXISTS target text NOT NULL DEFAULT 'server' CHECK (target IN ('server','device'));
ALTER TABLE kb.mcp_servers ADD COLUMN IF NOT EXISTS target_instance_id text NULL;

COMMENT ON COLUMN kb.mcp_servers.target IS 'server (connected outbound) or device (pushed over the relay)';
COMMENT ON COLUMN kb.mcp_servers.target_instance_id IS 'relay instance_id for device target; NULL = all eligible devices in the project';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE kb.mcp_servers DROP COLUMN IF EXISTS target_instance_id;
ALTER TABLE kb.mcp_servers DROP COLUMN IF EXISTS target;
-- +goose StatementEnd
