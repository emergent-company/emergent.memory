-- +goose Up
-- Drop the FK constraint on session_todos.session_id.
-- session_todos should work with any session ID (acp, adk, graph, etc.)
-- not just those that exist in kb.acp_sessions.
ALTER TABLE kb.session_todos DROP CONSTRAINT IF EXISTS session_todos_session_id_fkey;

-- +goose Down
ALTER TABLE kb.session_todos
    ADD CONSTRAINT session_todos_session_id_fkey
    FOREIGN KEY (session_id) REFERENCES kb.acp_sessions(id) ON DELETE CASCADE;
