-- +goose Up
-- +goose StatementBegin
-- Recipients an MCP share key was emailed to.
--
-- The key itself is never stored in this column: this is the attribution label
-- ("who holds this invite"), used to show who a share was sent to and to name
-- the grant in later invites. Addresses are lowercased and de-duplicated when
-- recorded. An empty array is a share that was never emailed.
ALTER TABLE core.mcp_share_instances
    ADD COLUMN IF NOT EXISTS recipient_emails TEXT[] NOT NULL DEFAULT '{}';

COMMENT ON COLUMN core.mcp_share_instances.recipient_emails IS 'Email addresses this share key was invited to (lowercased, de-duplicated); attribution label only, never a credential';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE core.mcp_share_instances DROP COLUMN IF EXISTS recipient_emails;
-- +goose StatementEnd
