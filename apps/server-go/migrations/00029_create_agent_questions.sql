-- +goose Up

-- Create kb.agent_questions table for human-in-the-loop agent interactions.
-- Agents can pause execution to ask users questions via the ask_user tool.
-- Questions are linked to agent runs and surfaced through the notification system.
CREATE TABLE IF NOT EXISTS kb.agent_questions (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id          UUID NOT NULL REFERENCES kb.agent_runs(id) ON DELETE CASCADE,
    agent_id        UUID NOT NULL REFERENCES kb.agents(id) ON DELETE CASCADE,
    project_id      UUID NOT NULL,
    question        TEXT NOT NULL,
    options         JSONB NOT NULL DEFAULT '[]',
    response        TEXT,
    responded_by    UUID,
    responded_at    TIMESTAMPTZ,
    status          TEXT NOT NULL DEFAULT 'pending',
    notification_id UUID,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

COMMENT ON TABLE kb.agent_questions IS 'Questions posed by agents to users via the ask_user tool during execution';
COMMENT ON COLUMN kb.agent_questions.options IS 'JSON array of {label, value, description?} for structured choices';
COMMENT ON COLUMN kb.agent_questions.status IS 'Question lifecycle: pending, answered, expired, cancelled';
COMMENT ON COLUMN kb.agent_questions.notification_id IS 'Link to the kb.notifications record created for this question';

-- Index for querying questions by run
CREATE INDEX IF NOT EXISTS idx_agent_questions_run_id ON kb.agent_questions(run_id);

-- Index for querying questions by agent
CREATE INDEX IF NOT EXISTS idx_agent_questions_agent_id ON kb.agent_questions(agent_id);

-- Composite index for listing pending questions per project
CREATE INDEX IF NOT EXISTS idx_agent_questions_project_status ON kb.agent_questions(project_id, status);

-- +goose Down

DROP TABLE IF EXISTS kb.agent_questions;
