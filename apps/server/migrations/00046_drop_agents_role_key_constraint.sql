-- +goose Up
-- +goose StatementBegin

-- The agents_role_key constraint enforces UNIQUE (strategy_type) globally,
-- which prevents creating more than one agent with the same strategy_type
-- (e.g. two "agentic" agents in different projects). strategy_type is a
-- category label, not a unique identifier. Drop the constraint entirely;
-- the btree index IDX_agents_strategy_type is retained for query performance.
ALTER TABLE kb.agents DROP CONSTRAINT IF EXISTS agents_role_key;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE kb.agents ADD CONSTRAINT agents_role_key UNIQUE (strategy_type);
-- +goose StatementEnd
