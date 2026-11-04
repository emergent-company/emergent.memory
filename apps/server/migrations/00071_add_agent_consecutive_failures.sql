-- +goose Up
-- +goose StatementBegin
ALTER TABLE kb.agents ADD COLUMN IF NOT EXISTS consecutive_failures INT DEFAULT 0 NOT NULL;

CREATE INDEX IF NOT EXISTS idx_agents_consecutive_failures
    ON kb.agents(consecutive_failures)
    WHERE consecutive_failures > 0;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS kb.idx_agents_consecutive_failures;
ALTER TABLE kb.agents DROP COLUMN IF EXISTS consecutive_failures;
-- +goose StatementEnd
