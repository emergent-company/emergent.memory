-- +goose Up
CREATE TABLE kb.adk_sessions (
    id TEXT NOT NULL,
    app_name TEXT NOT NULL,
    user_id TEXT NOT NULL,
    state JSONB DEFAULT '{}'::jsonb,
    create_time TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    update_time TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (app_name, user_id, id)
);

CREATE TABLE kb.adk_events (
    id TEXT NOT NULL,
    app_name TEXT NOT NULL,
    user_id TEXT NOT NULL,
    session_id TEXT NOT NULL,
    invocation_id TEXT,
    author TEXT,
    actions JSONB,
    long_running_tool_ids_json JSONB,
    branch TEXT,
    timestamp TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    content JSONB,
    grounding_metadata JSONB,
    custom_metadata JSONB,
    usage_metadata JSONB,
    citation_metadata JSONB,
    partial BOOLEAN,
    turn_complete BOOLEAN,
    error_code TEXT,
    error_message TEXT,
    interrupted BOOLEAN,
    PRIMARY KEY (id, app_name, user_id, session_id),
    CONSTRAINT adk_events_session_fk FOREIGN KEY (app_name, user_id, session_id) REFERENCES kb.adk_sessions (app_name, user_id, id) ON DELETE CASCADE
);

CREATE INDEX idx_adk_events_session ON kb.adk_events(app_name, user_id, session_id);
CREATE INDEX idx_adk_events_timestamp ON kb.adk_events(timestamp);

-- The task calls for kb.adk_states -> kb.adk_sessions foreign key constraint.
-- To satisfy this, kb.adk_states will store session, user, and app states,
-- using 'scope' to differentiate.
CREATE TABLE kb.adk_states (
    scope TEXT NOT NULL, -- 'app', 'user', or 'session'
    app_name TEXT NOT NULL,
    user_id TEXT NOT NULL DEFAULT '',
    session_id TEXT NOT NULL DEFAULT '',
    state JSONB DEFAULT '{}'::jsonb,
    update_time TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (scope, app_name, user_id, session_id)
);

-- When scope is 'session', the state belongs to a session
-- Wait, we can't easily do a conditional foreign key in postgres without a trigger or check.
-- We'll just enforce an index and we'll create a foreign key where applicable if needed,
-- but the simplest is just using a soft relationship or standard constraint if allowed.
-- To strictly follow 1.3: "Add foreign key constraints between kb.adk_events -> kb.adk_sessions and kb.adk_states -> kb.adk_sessions."
-- Let's make the foreign key on adk_states to adk_sessions, using MATCH SIMPLE or similar, or just allow it to fail if session doesn't exist.
-- Actually, a foreign key requires exact column match. Since user_id and session_id can be empty for 'app' and 'user' scopes,
-- a direct FK to (app_name, user_id, id) would fail for scope='app'.
-- Let's NOT add the FK to adk_states if it stores non-session states.
-- WAIT, ADK's `State` for sessions could be purely in `adk_sessions.state`.
-- If the task says "kb.adk_states -> kb.adk_sessions", maybe the design intended `adk_states` to ONLY hold session states (like a separate table for key-values)?
-- Or maybe the FK is ON DELETE CASCADE. Let's just create the constraint and allow NULLs for session_id if it's app/user scope.
-- Let's alter the table to use NULL instead of '' for user_id/session_id so FK allows it.
DROP TABLE kb.adk_states;

CREATE TABLE kb.adk_states (
    id UUID DEFAULT gen_random_uuid() PRIMARY KEY,
    scope TEXT NOT NULL, -- 'app', 'user', 'session'
    app_name TEXT NOT NULL,
    user_id TEXT,
    session_id TEXT,
    state JSONB DEFAULT '{}'::jsonb,
    update_time TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT adk_states_session_fk FOREIGN KEY (app_name, user_id, session_id) REFERENCES kb.adk_sessions (app_name, user_id, id) ON DELETE CASCADE
);

CREATE UNIQUE INDEX idx_adk_states_unique ON kb.adk_states(scope, app_name, COALESCE(user_id, ''), COALESCE(session_id, ''));

-- +goose Down
DROP TABLE IF EXISTS kb.adk_states;
DROP TABLE IF EXISTS kb.adk_events;
DROP TABLE IF EXISTS kb.adk_sessions;
