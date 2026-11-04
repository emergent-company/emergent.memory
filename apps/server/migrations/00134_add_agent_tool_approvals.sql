-- +goose Up
-- +goose StatementBegin

-- Agent tool approvals: audit trail for human-in-the-loop tool-policy
-- confirmations. One row per intercepted tool call; the decision column is
-- updated from 'pending' to 'approved'/'rejected'/'cancelled' on response.
CREATE TABLE IF NOT EXISTS kb.agent_tool_approvals (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id UUID NOT NULL,
    project_id UUID NOT NULL,
    agent_id UUID NOT NULL,
    question_id UUID NOT NULL,
    tool_name VARCHAR(255) NOT NULL,
    args_summary JSONB NOT NULL DEFAULT '{}',
    decision VARCHAR(20) NOT NULL DEFAULT 'pending',  -- 'pending', 'approved', 'rejected', 'cancelled'
    message TEXT,
    decided_by UUID,
    decided_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_agent_tool_approvals_run_id
    ON kb.agent_tool_approvals(run_id);
CREATE INDEX IF NOT EXISTS idx_agent_tool_approvals_project_id
    ON kb.agent_tool_approvals(project_id);
CREATE INDEX IF NOT EXISTS idx_agent_tool_approvals_question_id
    ON kb.agent_tool_approvals(question_id);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS kb.agent_tool_approvals;
-- +goose StatementEnd
