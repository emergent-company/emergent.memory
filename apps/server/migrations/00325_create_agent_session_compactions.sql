-- +goose Up
-- +goose StatementBegin

-- Ancestry record for session compression. Compression rewrites the ADK
-- session (delete + create, the only way to shrink an event log), so without
-- this row the fact that history was summarised is invisible: nothing records
-- which events were dropped, by which summary, or in what order. The durable
-- per-run transcript (kb.agent_run_messages / kb.agent_run_tool_calls) is
-- unaffected by compression; this table is the chain between the working
-- session and the runs that produced it.
CREATE TABLE IF NOT EXISTS kb.agent_session_compactions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id TEXT NOT NULL,
    seq INTEGER NOT NULL,
    dropped_events INTEGER NOT NULL,
    retained_events INTEGER NOT NULL,
    first_dropped_event_id TEXT,
    last_dropped_event_id TEXT,
    summary TEXT NOT NULL,
    summary_tokens INTEGER NOT NULL DEFAULT 0,
    model TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_agent_session_compactions_session_seq
    ON kb.agent_session_compactions (session_id, seq);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE IF EXISTS kb.agent_session_compactions;

-- +goose StatementEnd
