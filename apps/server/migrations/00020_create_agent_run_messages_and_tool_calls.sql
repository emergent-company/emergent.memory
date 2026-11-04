-- +goose Up
-- +goose StatementBegin

-- Agent run messages: stores the full LLM conversation history for each agent run.
-- Each message (system, user, assistant, tool_result) is persisted as it occurs during execution.
CREATE TABLE IF NOT EXISTS kb.agent_run_messages (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id UUID NOT NULL REFERENCES kb.agent_runs(id) ON DELETE CASCADE,
    role VARCHAR(20) NOT NULL,  -- 'system', 'user', 'assistant', 'tool_result'
    content JSONB NOT NULL DEFAULT '{}',  -- Full message content including tool call IDs, function names, arguments
    step_number INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Index for efficient message retrieval ordered by step
CREATE INDEX IF NOT EXISTS idx_agent_run_messages_run_step
    ON kb.agent_run_messages(run_id, step_number);

-- Agent run tool calls: records each tool invocation during agent execution.
-- Captures tool name, input/output, status, duration, and step number for full observability.
CREATE TABLE IF NOT EXISTS kb.agent_run_tool_calls (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id UUID NOT NULL REFERENCES kb.agent_runs(id) ON DELETE CASCADE,
    message_id UUID REFERENCES kb.agent_run_messages(id) ON DELETE SET NULL,
    tool_name VARCHAR(255) NOT NULL,
    input JSONB NOT NULL DEFAULT '{}',
    output JSONB NOT NULL DEFAULT '{}',
    status VARCHAR(20) NOT NULL DEFAULT 'completed',  -- 'completed', 'error'
    duration_ms INT,
    step_number INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Index for efficient tool call retrieval by run and step
CREATE INDEX IF NOT EXISTS idx_agent_run_tool_calls_run_step
    ON kb.agent_run_tool_calls(run_id, step_number);

-- Index for filtering tool calls by tool name within a run
CREATE INDEX IF NOT EXISTS idx_agent_run_tool_calls_run_tool
    ON kb.agent_run_tool_calls(run_id, tool_name);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS kb.agent_run_tool_calls;
DROP TABLE IF EXISTS kb.agent_run_messages;
-- +goose StatementEnd
