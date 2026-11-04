-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS kb.org_tool_settings (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id      UUID NOT NULL REFERENCES kb.orgs(id) ON DELETE CASCADE,
    tool_name   TEXT NOT NULL,
    enabled     BOOLEAN NOT NULL DEFAULT true,
    config      JSONB,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT current_timestamp,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT current_timestamp,
    UNIQUE(org_id, tool_name)
);

CREATE INDEX IF NOT EXISTS idx_org_tool_settings_org_id
    ON kb.org_tool_settings(org_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS kb.org_tool_settings;
-- +goose StatementEnd
