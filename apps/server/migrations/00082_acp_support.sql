-- +goose Up

-- ACP sessions: thin grouping of related agent runs
CREATE TABLE IF NOT EXISTS kb.acp_sessions (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id  UUID NOT NULL REFERENCES kb.projects(id) ON DELETE CASCADE,
    agent_name  TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_acp_sessions_project_id ON kb.acp_sessions(project_id);

-- ACP run events: persisted SSE events for GET /runs/:runId/events
CREATE TABLE IF NOT EXISTS kb.acp_run_events (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id      UUID NOT NULL REFERENCES kb.agent_runs(id) ON DELETE CASCADE,
    event_type  TEXT NOT NULL,
    data        JSONB NOT NULL DEFAULT '{}',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_acp_run_events_run_id_created ON kb.acp_run_events(run_id, created_at);

-- Link agent runs to ACP sessions
ALTER TABLE kb.agent_runs ADD COLUMN IF NOT EXISTS acp_session_id UUID REFERENCES kb.acp_sessions(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_agent_runs_acp_session_id ON kb.agent_runs(acp_session_id);

-- +goose Down

ALTER TABLE kb.agent_runs DROP COLUMN IF EXISTS acp_session_id;
DROP TABLE IF EXISTS kb.acp_run_events;
DROP TABLE IF EXISTS kb.acp_sessions;
